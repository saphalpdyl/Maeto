package maetoportal

import "context"

type Tunnel interface {
	Up(ctx context.Context) error
	Down(ctx context.Context) error
}
