package maetoportal

import (
	"context"
	"os"

	"github.com/saphalpdyl/maeto/services/maeto-portal/log"
)

func (p *Portal) loadConnection(
	ctx context.Context,
	remoteAddr string,
	localID string,
	remoteID string,
) error {
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

	err = p.swanClient.LoadConnection(
		ctx,
		[]string{"%any"},
		[]string{remoteAddr},
		localID,
		remoteID,
		"pop",
		"pop",
		certData,
		caCertData,
	)
	if err != nil {
		return err
	}

	p.logger.InfoContext(ctx, "connection loaded successfully")

	return nil
}
