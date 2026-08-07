package maetoagent

import (
	"context"
	"net"
	"net/http"
)

func NewControlClient(
	network, address string,
) *http.Client {
	if network == "unix" {
		return &http.Client{
			Transport: &http.Transport{
				ForceAttemptHTTP2: true,
				DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", addr)
				},
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", addr)
				},
			},
		}
	}

	return http.DefaultClient
}
