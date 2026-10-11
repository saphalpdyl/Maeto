package maetoportal

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/saphalpdyl/maeto/libs/swan"
	"github.com/saphalpdyl/maeto/services/maeto-portal/log"
	"github.com/strongswan/govici/vici"
)

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
