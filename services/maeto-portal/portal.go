package maetoportal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/saphalpdyl/maeto/libs/controlapi"
	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/intentkv"
	"github.com/saphalpdyl/maeto/libs/swan"
	"github.com/saphalpdyl/maeto/services/maeto-portal/log"
	"github.com/strongswan/govici/vici"
)

const (
	initiateTimeout    = 10 * time.Second
	initiateBackoffMin = 1 * time.Second
	initiateBackoffMax = 30 * time.Second
)

type Portal struct {
	InstanceId string

	js jetstream.JetStream

	// Intents pushed to by the intentkv watcher and read by the Reconciler
	intentFeed chan *dataplane.NodeIntent
	reconciler *dataplane.Reconciler

	config PortalConfig
	logger *slog.Logger
}

func NewPortal(
	instanceId string,
	js jetstream.JetStream,
	config PortalConfig,
	dp dataplane.Dataplane,
	logger *slog.Logger,
) *Portal {
	intentFeed := make(chan *dataplane.NodeIntent, 32)
	reconciler := dataplane.NewReconciler(
		dp,
		dataplane.NodeTypeCPE,
		logger.With(log.Domain(log.DomainReconciler)),
		intentFeed,
	)

	return &Portal{
		InstanceId: instanceId,
		config:     config,
		logger:     logger,
		js:         js,
		reconciler: reconciler,
		intentFeed: intentFeed,
	}
}

func (p *Portal) Run(ctx context.Context) error {
	p.logger.InfoContext(ctx, "portal starting", log.PortalID(p.config.PortalID))

	if !p.waitForReady(ctx) {
		p.logger.ErrorContext(ctx, "portal failed to become ready")
		return nil
	}

	p.logger.InfoContext(ctx, "portal ready", log.PortalID(p.config.PortalID))

	s, err := vici.NewSession()
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to open vici session", log.Err(err))
		return err
	}
	defer s.Close() // nolint:errcheck

	eventChan, err := p.watchEvents(ctx, s)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to listen to vici child-updown event", log.Err(err))
		return err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-eventChan:
				if len(e.IKE.ChildSAs) != 1 {
					p.logger.WarnContext(ctx, fmt.Sprintf("child-updown: expected 1 child SA, got %d", len(e.IKE.ChildSAs)))
					continue
				}

				var cSa ChildSA
				for _, v := range e.IKE.ChildSAs {
					cSa = v
				}

				ifID, err := cSa.IfID()
				if err != nil {
					p.logger.ErrorContext(ctx, "failed to parse IfID into uint32", log.Err(err))
					continue
				}

				req := controlapi.CPETunnelUpdateRequest{
					PortalID: p.config.PortalID,
					IfID:     ifID,
				}

				data, err := json.Marshal(req)
				if err != nil {
					p.logger.ErrorContext(ctx, "failed to marshal cpe tunnel update request", log.Err(err))
					continue
				}

				respData, err := p.js.Conn().Request(controlapi.SubjectCPETunnelUpdate, data, 5*time.Second)

				if err != nil {
					p.logger.ErrorContext(ctx, "failed to publish cpe tunnel update request", log.Err(err))
					continue
				}

				p.logger.InfoContext(ctx, "cpe tunnel update request published",
					log.PortalID(p.config.PortalID),
					slog.Int("if_id", int(ifID)),
				)

				var resp controlapi.TunnelUpdateResponse
				if err := json.Unmarshal(respData.Data, &resp); err != nil {
					p.logger.ErrorContext(ctx, "failed to unmarshal cpe tunnel update response", log.Err(err))
					continue
				}

				if !resp.Ok {
					p.logger.ErrorContext(ctx, "cpe tunnel update failed")
					continue
				}

				p.logger.InfoContext(ctx, "cpe tunnel update successful",
					log.PortalID(p.config.PortalID),
					slog.Int("if_id", int(ifID)),
				)
			}
		}
	}()

	swanConnOptsReq := controlapi.PortalAuthEndpointRequest{
		PortalID: p.config.PortalID,
	}
	swanConnOptsReqData, err := json.Marshal(swanConnOptsReq)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to marshal portal auth request", log.Err(err))
		return err
	}

	data, err := p.js.Conn().Request(controlapi.SubjectPortalAuthIdentity, swanConnOptsReqData, 3*time.Second)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to request portal auth identity", log.Err(err))
		return err
	}

	var swanConnOptsResp controlapi.PortalAuthEndpointResponse
	if err := json.Unmarshal(data.Data, &swanConnOptsResp); err != nil {
		p.logger.ErrorContext(ctx, "failed to unmarshal portal auth response", log.Err(err))
		return err
	}

	p.logger.InfoContext(ctx, "received portal auth response",
		log.PortalID(p.config.PortalID),
	)

	// TODO: Generate credentials in the control plane signed by the CA
	if err := p.loadCredentials(ctx, s); err != nil {
		return err
	}

	if err := p.loadConnection(ctx, s, swanConnOptsResp.AttachNodeAddr,
		swanConnOptsResp.LocalSwanIdentity, swanConnOptsResp.RemoteSwanIdentity); err != nil {
		return err
	}

	if err := p.initiateTunnel(ctx, s, "pop"); err != nil {
		return err
	}

	go func() {
		err := p.reconciler.Start(ctx)
		if err != nil {
			p.logger.ErrorContext(ctx, "reconciler failed to start", log.Err(err))
		}
	}()

	go func() {
		err := intentkv.Watch(
			ctx,
			p.js,
			p.logger.With(log.Domain(log.DomainControlPlane)),
			intentkv.Key(intentkv.PrefixCPE, p.config.PortalID),
			p.intentFeed,
		)

		if err != nil {
			p.logger.ErrorContext(ctx, "intent watch failed",
				log.Domain(log.DomainControlPlane),
				log.Err(err),
			)
		}
	}()

	<-ctx.Done()

	return p.shutdown(ctx)
}

func (p *Portal) shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.config.ShutdownGracePeriod)
	defer cancel()

	p.logger.InfoContext(ctx, "portal shutting down",
		log.DurationMs(p.config.ShutdownGracePeriod.Milliseconds()),
	)

	return nil
}

func (p *Portal) waitForReady(ctx context.Context) bool {
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		attempt++

		data, err := p.js.Conn().Request(controlapi.SubjectHealthReady, nil, time.Second)
		if err != nil {
			p.logger.WarnContext(ctx, "control plane unreachable",
				log.Domain(log.DomainControlPlane),
				log.Attempt(attempt),
				log.Err(err),
			)
		} else {
			var resp struct {
				Ready string `json:"ready"`
			}

			if err = json.Unmarshal(data.Data, &resp); err != nil {
				p.logger.ErrorContext(ctx, "failed to parse health response",
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

func (p *Portal) loadCredentials(ctx context.Context, s *vici.Session) error {
	privateKeyData, err := os.ReadFile("/etc/swanctl/private/key.pem")
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to read private key", log.Err(err))
		return err
	}

	msg, err := vici.MarshalMessage(swan.LoadKeyRequest{
		Type: "any",
		Data: string(privateKeyData),
	})
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to marshal load key request", log.Err(err))
		return err
	}

	res, err := s.Call(ctx, "load-key", msg)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to load key", log.Err(err))
		return err
	}

	if res.Err() != nil {
		p.logger.ErrorContext(ctx, "load key failed", log.Err(res.Err()))
		return res.Err()
	}

	p.logger.InfoContext(ctx, fmt.Sprintf("key loaded successfully: %s", res.String()))

	return nil
}

func (p *Portal) loadConnection(ctx context.Context, s *vici.Session, remoteAddr string, localID string, remoteID string) error {
	certData, err := os.ReadFile("/etc/swanctl/x509/cert.pem")
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to read certificate", log.Err(err))
		return err
	}

	caCertData, err := os.ReadFile("/etc/swanctl/x509ca/ca-cert.pem")
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to read CA certificate", log.Err(err))
		return err
	}

	req := swan.LoadConnRequest{
		"pop": swan.ConnConf{
			Version:     "2",
			LocalAddrs:  []string{"%any"},
			RemoteAddrs: []string{remoteAddr},
			KeyingTries: "0",
			Local: swan.AuthConf{
				Auth:  "pubkey",
				Certs: []string{string(certData)},
				ID:    localID,
			},
			Remote: swan.AuthConf{
				Auth:    "pubkey",
				CACerts: []string{string(caCertData)},
				ID:      remoteID,
			},
			Children: map[string]swan.ChildConf{
				"pop": {
					Mode:        "tunnel",
					LocalTS:     []string{"::/0"},
					RemoteTS:    []string{"::/0"},
					IfIDIn:      "%unique",
					IfIDOut:     "%unique",
					StartAction: "none",
				},
			},
		},
	}

	msg, err := vici.MarshalMessage(req)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to marshal load connection request", log.Err(err))
		return err
	}

	res, err := s.Call(ctx, "load-conn", msg)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to load connection", log.Err(err))
		return err
	}

	if res.Err() != nil {
		p.logger.ErrorContext(ctx, "load connection failed", log.Err(res.Err()))
		return res.Err()
	}

	p.logger.InfoContext(ctx, fmt.Sprintf("connection loaded successfully: %s", res.String()))

	return nil
}

func (p *Portal) initiateTunnel(ctx context.Context, s *vici.Session, child string) error {
	msg, err := vici.MarshalMessage(swan.InitiateRequest{
		Child:   child,
		Timeout: strconv.FormatInt(initiateTimeout.Milliseconds(), 10),
	})
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to marshal initiate request", log.Err(err))
		return err
	}

	backoff := initiateBackoffMin

	for attempt := 1; ; attempt++ {
		res, err := s.Call(ctx, "initiate", msg)
		if err == nil {
			err = res.Err()
		}

		if err == nil {
			p.logger.InfoContext(ctx, "tunnel initiated",
				slog.String("child", child),
				log.Attempt(attempt),
			)
			return nil
		}

		p.logger.WarnContext(ctx, "tunnel initiation failed",
			slog.String("child", child),
			log.Attempt(attempt),
			log.Err(err),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		if backoff *= 2; backoff > initiateBackoffMax {
			backoff = initiateBackoffMax
		}
	}
}

func (p *Portal) watchEvents(ctx context.Context, s *vici.Session) (<-chan *UpDownEvent, error) {
	ec := make(chan vici.Event, 16)
	s.NotifyEvents(ec)

	outChan := make(chan *UpDownEvent, 16)

	if err := s.Subscribe("child-updown"); err != nil {
		p.logger.WarnContext(ctx, "ike-updown EVENT failed to subscribe", log.Err(err))
		return nil, err
	}

	go func() {
		defer s.StopEvents(ec)

		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-ec:
				if !ok {
					p.logger.InfoContext(ctx, "ike/child even not ok")
					return
				}

				parsedEvent, err := ParseUpDown(e.Name, e.Message)
				if err != nil {
					p.logger.ErrorContext(ctx, "failed to parse vici updown event", log.Err(err))
					continue
				}

				outChan <- parsedEvent
			}
		}
	}()

	return outChan, nil
}
