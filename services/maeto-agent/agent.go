package maetoagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/strongswan/govici/vici"

	"github.com/saphalpdyl/maeto/libs/controlapi"
	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/intentkv"
	"github.com/saphalpdyl/maeto/libs/swan"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

type Agent struct {
	js         jetstream.JetStream
	node       *Node
	logger     *slog.Logger
	reconciler *dataplane.Reconciler
	dp         dataplane.Dataplane // owned primarily by the Reconciler

	// Intents pushed to by the intentkv watcher and read by the Reconciler
	intentFeed chan *dataplane.NodeIntent
}

func NewAgent(node *Node, js jetstream.JetStream, logger *slog.Logger, dp dataplane.Dataplane) *Agent {
	intentFeed := make(chan *dataplane.NodeIntent, 32)
	reconciler := dataplane.NewReconciler(dp, dataplane.NodeTypePE, logger.With(log.Domain(log.DomainReconciler)), intentFeed)

	return &Agent{
		js:         js,
		node:       node,
		logger:     logger,
		reconciler: reconciler,
		intentFeed: intentFeed,
		dp:         dp,
	}
}

func (a *Agent) Node() *Node {
	return a.node
}

func (a *Agent) Run(ctx context.Context) {
	a.logger.InfoContext(ctx, "agent starting",
		log.NodeName(a.node.Name),
		log.Locator(a.node.Locator),
		slog.Int("core_links", len(a.node.CoreInterfaces())),
		slog.Bool("access_side", a.node.HasAccessSide()),
	)

	if !a.waitForReady(ctx) {
		return
	}

	a.logger.InfoContext(ctx, "control plane ready",
		log.Domain(log.DomainControlPlane),
		slog.String("intent_key", a.node.IntentKey()),
	)

	go a.reconciler.Start(ctx) // nolint:errcheck

	go func() {
		if err := intentkv.Watch(ctx, a.js, a.logger.With(log.Domain(log.DomainControlPlane)),
			intentkv.Key(intentkv.PrefixPE, a.node.ID), a.intentFeed); err != nil {
			a.logger.ErrorContext(ctx, "intent watch failed",
				log.Domain(log.DomainControlPlane),
				slog.String("intent_key", a.node.IntentKey()),
				log.Err(err),
			)
		}
	}()

	s, err := vici.NewSession()
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to open vici session", log.Err(err))
		return
	}
	defer s.Close() // nolint:errcheck

	eventChan, err := a.watchEvents(ctx, s)
	if err != nil {
		return
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-eventChan:
				if e.Event != "child-updown" {
					continue
				}

				if len(e.IKE.ChildSAs) != 1 {
					a.logger.WarnContext(ctx, fmt.Sprintf("child-updown: expected 1 child SA, got %d", len(e.IKE.ChildSAs)))
					continue
				}

				var cSa ChildSA
				for _, v := range e.IKE.ChildSAs {
					cSa = v
				}

				// X.509 cert of CPE to get SAN for portalID
				cert, err := e.RemoteCertificate()
				if err != nil {
					a.logger.ErrorContext(ctx, "failed to get remote certificate", log.Err(err))
					continue
				}

				ifID, err := cSa.IfID()
				if err != nil {
					a.logger.ErrorContext(ctx, "invalid child sa if_id", log.Err(err))
					continue
				}

				// Assume a <portal_id>.cpe.maeto.net format
				// *.cpe.maeto.net is already verified by strongswan, so we can trust the SAN
				cpeSAN := cert.DNSNames[0]
				sanParts := strings.Split(cpeSAN, ".")
				if len(sanParts) <= 0 {
					a.logger.ErrorContext(ctx, "invalid SAN format", slog.String("san", cpeSAN))
					continue
				}

				portalID := sanParts[0]

				a.logger.InfoContext(ctx, "got child updown event SAN", slog.Any("san", cpeSAN), slog.Any("if_id", ifID), slog.Any("portal_id", portalID))

				// Push the connection event to the control plane
				// 	The control plane then pushes to ServiceRegistry
				// 	and delivers a new NodeIntent to reconcile on.
				// So connection -> push_to_controller -> new intent -> reconcile -> FIB updated
				var req controlapi.PETunnelUpdateRequest
				req.PortalID = portalID
				req.IfID = ifID
				req.NodeID = a.node.ID

				data, err := json.Marshal(req)
				if err != nil {
					a.logger.ErrorContext(ctx, "failed to marshal request", log.Err(err))
					continue
				}

				resp, err := a.js.Conn().Request(controlapi.SubjectPETunnelUpdate, data, 5*time.Second)
				if err != nil {
					a.logger.ErrorContext(ctx, "failed to send push tunnel initiate request", log.Err(err))
					continue
				}

				var pushResp controlapi.TunnelUpdateResponse
				if err := json.Unmarshal(resp.Data, &pushResp); err != nil {
					a.logger.ErrorContext(ctx, "failed to unmarshal push tunnel initiate response", log.Err(err))
					continue
				}

				if !pushResp.Ok {
					a.logger.ErrorContext(ctx, "push tunnel initiate request failed", slog.String("portal_id", portalID))
					continue
				}

				a.logger.InfoContext(ctx, "successfully sent req", slog.Any("data", req))
			}
		}

	}()

	if a.node.Access != nil {
		// Loads private key from /etc/swanctl/private/key.pem and sends to charon via vici
		if err := a.loadCredentials(ctx, s); err != nil {
			return
		}

		// Loads connection from /etc/swanctl/x509/cert.pem and /etc/swanctl/x509ca/ca-cert.pem and sends to charon via vici
		if err := a.loadConnection(ctx, s); err != nil {
			return
		}
	}

	<-ctx.Done()
}

func (a *Agent) watchEvents(ctx context.Context, s *vici.Session) (<-chan *UpDownEvent, error) {
	viciEventChan := make(chan vici.Event, 16)
	s.NotifyEvents(viciEventChan)

	eventChan := make(chan *UpDownEvent, 32)

	if err := s.Subscribe("ike-updown", "child-updown"); err != nil {
		a.logger.WarnContext(ctx, "ike-updown EVENT failed to subscribe", log.Err(err))
		return nil, err
	}

	go func() {
		defer s.StopEvents(viciEventChan)

		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-viciEventChan:
				if !ok {
					a.logger.InfoContext(ctx, "ike/child even not ok")
					continue
				}

				parsedEvent, err := ParseUpDown(e.Name, e.Message)
				if err != nil {
					a.logger.ErrorContext(ctx, "failed to parse vici updown event", log.Err(err))
					continue
				}

				eventChan <- parsedEvent
			}
		}
	}()

	return eventChan, nil
}

// charon runs as the ipsec user and cannot read the key itself, so the agent
// reads it and ships the pem over vici
func (a *Agent) loadCredentials(ctx context.Context, s *vici.Session) error {
	privateKeyData, err := os.ReadFile("/etc/swanctl/private/key.pem")
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to read private key", log.Err(err))
		return err
	}

	msg, err := vici.MarshalMessage(swan.LoadKeyRequest{
		Type: "any",
		Data: string(privateKeyData),
	})
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to marshal load key request", log.Err(err))
		return err
	}

	res, err := s.Call(ctx, "load-key", msg)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to load key", log.Err(err))
		return err
	}

	if res.Err() != nil {
		a.logger.ErrorContext(ctx, "load key failed", log.Err(res.Err()))
		return res.Err()
	}

	a.logger.InfoContext(ctx, fmt.Sprintf("key loaded successfully: %s", res.String()))

	return nil
}

func (a *Agent) loadConnection(ctx context.Context, s *vici.Session) error {
	certData, err := os.ReadFile("/etc/swanctl/x509/cert.pem")
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to read certificate", log.Err(err))
		return err
	}

	caCertData, err := os.ReadFile("/etc/swanctl/x509ca/ca-cert.pem")
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to read CA certificate", log.Err(err))
		return err
	}

	req := swan.LoadConnRequest{
		"cpe": swan.ConnConf{
			Version:     "2",
			LocalAddrs:  []string{a.node.Access.Address},
			RemoteAddrs: []string{"%any"},
			KeyingTries: "0",
			Local: swan.AuthConf{
				Auth:  "pubkey",
				Certs: []string{string(certData)},
				ID:    a.node.LocalSwanID(),
			},
			Remote: swan.AuthConf{
				Auth:    "pubkey",
				CACerts: []string{string(caCertData)},
				ID:      "*.cpe.maeto.net",
			},
			Children: map[string]swan.ChildConf{
				"cpe": {
					Mode:        "tunnel",
					LocalTS:     []string{"::/0"},
					RemoteTS:    []string{"::/0"},
					IfIDIn:      "%unique",
					IfIDOut:     "%unique",
					StartAction: "none", // remote_addrs = %any means nothing to initiate towards
				},
			},
		},
	}

	msg, err := vici.MarshalMessage(req)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to marshal load connection request", log.Err(err))
		return err
	}

	res, err := s.Call(ctx, "load-conn", msg)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to load connection", log.Err(err))
		return err
	}

	if res.Err() != nil {
		a.logger.ErrorContext(ctx, "load connection failed", log.Err(res.Err()))
		return res.Err()
	}

	a.logger.InfoContext(ctx, fmt.Sprintf("connection loaded successfully: %s", res.String()))

	return nil
}

// Waits for the control plane to reach healthy status
func (a *Agent) waitForReady(ctx context.Context) bool {
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		attempt++

		data, err := a.js.Conn().Request(controlapi.SubjectHealthReady, nil, time.Second)
		if err != nil {
			a.logger.WarnContext(ctx, "control plane unreachable",
				log.Domain(log.DomainControlPlane),
				log.Attempt(attempt),
				log.Err(err),
			)
		} else {
			var resp struct {
				Ready string `json:"ready"`
			}

			if err = json.Unmarshal(data.Data, &resp); err != nil {
				a.logger.ErrorContext(ctx, "failed to parse health response",
					log.Domain(log.DomainControlPlane),
					log.Err(err),
				)
			} else if resp.Ready == "true" {
				return true
			}
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(2000 * time.Millisecond):
		}
	}
}
