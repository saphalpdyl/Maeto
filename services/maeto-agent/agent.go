package maetoagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/strongswan/govici/vici"

	"github.com/saphalpdyl/maeto/libs/controlapi"
	"github.com/saphalpdyl/maeto/libs/swan"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

type Agent struct {
	js     jetstream.JetStream
	node   *Node
	logger *slog.Logger
}

func NewAgent(node *Node, js jetstream.JetStream, logger *slog.Logger) *Agent {
	return &Agent{
		js:     js,
		node:   node,
		logger: logger,
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

	// go func() {
	// 	if err := a.WatchIntents(ctx, a.js); err != nil {
	// 		a.logger.ErrorContext(ctx, "intent watch failed",
	// 			log.Domain(log.DomainControlPlane),
	// 			slog.String("intent_key", a.node.IntentKey()),
	// 			log.Err(err),
	// 		)
	// 	}
	// }()

	s, err := vici.NewSession()
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to open vici session", log.Err(err))
		return
	}
	defer s.Close() // nolint:errcheck

	if err := a.watchEvents(ctx, s); err != nil {
		return
	}

	if err := a.loadCredentials(ctx, s); err != nil {
		return
	}

	if err := a.loadConnection(ctx, s); err != nil {
		return
	}

	<-ctx.Done()
}

func (a *Agent) watchEvents(ctx context.Context, s *vici.Session) error {
	ec := make(chan vici.Event, 16)
	s.NotifyEvents(ec)

	if err := s.Subscribe("ike-updown", "child-updown"); err != nil {
		a.logger.WarnContext(ctx, "ike-updown EVENT failed to subscribe", log.Err(err))
		return err
	}

	go func() {
		defer s.StopEvents(ec)

		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-ec:
				if !ok {
					a.logger.InfoContext(ctx, "ike/child even not ok")
					return
				}

				a.logger.InfoContext(ctx, fmt.Sprintf("IKE/CHILD Event: %s", e.Message.String()))
			}
		}
	}()

	return nil
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
