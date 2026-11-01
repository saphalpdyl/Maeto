package maetoportal

import (
	"context"
	"os"

	"github.com/saphalpdyl/maeto/services/maeto-portal/log"
)

func (p *Portal) loadCredentials(ctx context.Context) error {
	privateKeyData, err := os.ReadFile("/etc/swanctl/private/key.pem")
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to read private key", log.Err(err))
		return err
	}

	err = p.swanClient.LoadKey(ctx, privateKeyData)
	if err != nil {
		return err
	}

	p.logger.InfoContext(ctx, "key loaded successfully:")

	return nil
}
