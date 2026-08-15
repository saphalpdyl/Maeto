package maetoportal

import "context"

type ControlClient interface {
	Connect(ctx context.Context) error
	Close() error
}
