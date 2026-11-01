package swan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/saphalpdyl/maeto/libs/swan/log"
	"github.com/strongswan/govici/vici"
)

const (
	initiateTimeout    = 10 * time.Second
	initiateBackoffMin = 1 * time.Second
	initiateBackoffMax = 30 * time.Second
)

type ViciCommand string

const (
	ViciCommandListSAs        ViciCommand = "list-sas"
	ViciCommandLoadConnection ViciCommand = "load-conn"
	ViciCommandInitiate       ViciCommand = "initiate"
	ViciCommandLoadKey        ViciCommand = "load-key"
)

// Client is a high-level wrapper for managing Strongswan VICI sessions and streaming
// events back
type Client struct {
	s      *vici.Session
	logger *slog.Logger
}

func NewClient(logger *slog.Logger) (*Client, error) {
	s, err := vici.NewSession()
	if err != nil {
		return nil, err
	}

	return &Client{
		s:      s,
		logger: logger,
	}, nil
}

func NewClientWithSession(s *vici.Session, logger *slog.Logger) *Client {
	return &Client{
		s:      s,
		logger: logger,
	}
}

func (c *Client) WatchIKEUpdownEvents(ctx context.Context) (<-chan *UpDownEvent, error) {
	ec := make(chan vici.Event, 16)
	c.s.NotifyEvents(ec)

	outChan := make(chan *UpDownEvent, 16)
	if err := c.s.Subscribe("child-updown"); err != nil {
		c.logger.ErrorContext(ctx, "ike-updown EVENT failed to subscribe", log.Err(err))
		return nil, err
	}

	go func() {
		defer c.s.StopEvents(ec)

		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-ec:
				if !ok {
					c.logger.ErrorContext(ctx, "ike-updown event not ok")
					continue
				}

				parsedEvent, err := ParseUpDownEvent(e.Name, e.Message)
				if err != nil {
					c.logger.ErrorContext(ctx, "ike-updown event failed to parse", log.Err(err))
					continue
				}

				outChan <- parsedEvent
			}
		}
	}()

	return outChan, nil
}

func (c *Client) InitiateTunnel(
	ctx context.Context,
	child string,
) error {
	msg, err := vici.MarshalMessage(InitiateRequest{
		Child:   child,
		Timeout: strconv.FormatInt(initiateTimeout.Milliseconds(), 10),
	})
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to marshal initiate request", log.Err(err))
		return err
	}

	backoff := initiateBackoffMin

	for attempt := 1; ; attempt++ {
		res, err := c.s.Call(ctx, string(ViciCommandInitiate), msg)
		if err == nil {
			err = res.Err()
		}

		if err == nil {
			c.logger.InfoContext(ctx, "tunnel initiated",
				slog.String("child", child),
			)
			return nil
		}

		c.logger.WarnContext(ctx, "tunnel initiation failed",
			slog.String("child", child),
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

func (c *Client) LoadConnection(
	ctx context.Context,
	localAddrs []string,
	remoteAddrs []string,
	localID string,
	remoteID string,
	childName string,
	confName string,
	certData []byte,
	caCertData []byte,
) error {
	req := LoadConnRequest{
		confName: ConnConf{
			Version:     "2",
			LocalAddrs:  localAddrs,
			RemoteAddrs: remoteAddrs,
			IfIDIn:      "%unique",
			IfIDOut:     "%unique",
			KeyingTries: "0",
			Local: AuthConf{
				Auth:  "pubkey",
				Certs: []string{string(certData)},
				ID:    localID,
			},
			Remote: AuthConf{
				Auth:    "pubkey",
				CACerts: []string{string(caCertData)},
				ID:      remoteID,
			},
			Children: map[string]ChildConf{
				childName: {
					Mode:        "tunnel",
					LocalTS:     []string{"::/0"},
					RemoteTS:    []string{"::/0"},
					StartAction: "none",
				},
			},
		},
	}

	msg, err := vici.MarshalMessage(req)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to marshal load connection request", log.Err(err))
		return err
	}

	res, err := c.s.Call(ctx, string(ViciCommandLoadConnection), msg)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to load connection", log.Err(err))
		return err
	}

	if res.Err() != nil {
		c.logger.ErrorContext(ctx, "load connection failed", log.Err(res.Err()))
		return res.Err()
	}

	c.logger.InfoContext(ctx, fmt.Sprintf("connection loaded successfully: %s", res.String()))

	return nil
}

func (c *Client) GetSAs(ctx context.Context) ([]IKESA, error) {
	result := c.s.CallStreaming(ctx, string(ViciCommandListSAs), "list-sa", nil)

	sas := make([]IKESA, 0)
	viciErrors := make([]error, 0)

	for m, err := range result {
		if err != nil {
			viciErrors = append(viciErrors, err)
			continue
		}

		conn, section, ok := connSection(m)
		if !ok {
			c.logger.ErrorContext(ctx, "list-sa event has no connection section")
			continue
		}

		var sa IKESA
		if err := vici.UnmarshalMessage(section, &sa); err != nil {
			c.logger.ErrorContext(ctx, "failed to unmarshal ike-sa", slog.String("conn", conn), log.Err(err))
			continue
		}

		sas = append(sas, sa)
	}

	return sas, errors.Join(viciErrors...)
}

func (c *Client) LoadKey(ctx context.Context, privateKey []byte) error {
	msg, err := vici.MarshalMessage(LoadKeyRequest{
		Type: "any",
		Data: string(privateKey),
	})
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to marshal load key request", log.Err(err))
		return err
	}

	res, err := c.s.Call(ctx, string(ViciCommandLoadKey), msg)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to load key", log.Err(err))
		return err
	}

	if res.Err() != nil {
		c.logger.ErrorContext(ctx, "load key failed", log.Err(res.Err()))
		return res.Err()
	}

	return nil
}
