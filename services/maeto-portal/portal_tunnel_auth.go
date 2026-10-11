package maetoportal

import (
	"context"
	"fmt"
	"os"

	"github.com/saphalpdyl/maeto/libs/swan"
	"github.com/saphalpdyl/maeto/services/maeto-portal/log"
	"github.com/strongswan/govici/vici"
)

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
