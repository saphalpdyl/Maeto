package maetoagent

import (
	"context"
	"fmt"
	"os"

	"github.com/strongswan/govici/vici"

	"github.com/saphalpdyl/maeto/libs/swan"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

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
