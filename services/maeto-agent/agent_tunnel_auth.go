package maetoagent

import (
	"context"
	"os"

	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

// charon runs as the ipsec user and cannot read the key itself, so the agent
// reads it and ships the pem over vici
func (a *Agent) loadCredentials(ctx context.Context) error {
	privateKeyData, err := os.ReadFile("/etc/swanctl/private/key.pem")
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to read private key", log.Err(err))
		return err
	}

	err = a.swanClient.LoadKey(ctx, privateKeyData)
	if err != nil {
		return err
	}

	a.logger.InfoContext(ctx, "key loaded successfully")

	return nil
}

func (a *Agent) loadConnection(ctx context.Context) error {
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

	err = a.swanClient.LoadConnection(
		ctx,
		[]string{a.node.Access.Address},
		[]string{"%any"},
		a.node.LocalSwanID(),
		"*.cpe.maeto.net",
		"cpe",
		"cpe",
		certData,
		caCertData,
	)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to load connection", log.Err(err))
		return err
	}

	a.logger.InfoContext(ctx, "connection loaded successfully")

	return nil
}
