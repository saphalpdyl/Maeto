package dataplane

import (
	"context"
	"log/slog"
	"net/netip"
	"time"

	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

type NodeType string

const (
	NodeTypeCPE NodeType = "cpe"
	NodeTypePE  NodeType = "pe"
)

type NodeIntent struct {
	NodeType   NodeType  `json:"node_type"`
	Intent     Intent    `json:"intent"`
	Timestamp  time.Time `json:"timestamp"`
	Generation uint32    `json:"generation"`
	Version    uint32    `json:"version"`
}

type Intent interface {
	GetNodeType() NodeType
}

// High-level intent instructions that get converted to dataplane instructions
// by the reconciler
type CPEIntent struct {
	TunnelInterfaceID    uint32       `json:"tunnel_interface_id"`
	TunnelPE             string       `json:"tunnel_pe"`
	TunnelPEEndpointAddr netip.Addr   `json:"tunnel_pe_endpoint_addr"`
	TenantID             string       `json:"tenant_id"`
	TenantPrefix         netip.Prefix `json:"tenant_prefix"`
	SitePrefix           netip.Prefix `json:"site_prefix"`
}

func (i *CPEIntent) GetNodeType() NodeType {
	return NodeTypeCPE
}

type PEIntent struct{}

func (i *PEIntent) GetNodeType() NodeType {
	return NodeTypePE
}

var _ Intent = (*CPEIntent)(nil)
var _ Intent = (*PEIntent)(nil)

////////////////////////////////////////////////////////////////

type Reconciler struct {
	dp Dataplane

	intentFeed <-chan *NodeIntent

	logger *slog.Logger
}

func NewReconciler(dp Dataplane, logger *slog.Logger, intentFeed <-chan *NodeIntent) *Reconciler {
	return &Reconciler{
		dp:         dp,
		intentFeed: intentFeed,
		logger:     logger,
	}
}

func (r *Reconciler) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case intent := <-r.intentFeed:
			r.logger.InfoContext(ctx, "reconciler received intent", "intent", intent)
			if err := r.Reconcile(ctx, intent); err != nil {
				r.logger.ErrorContext(ctx, "reconcile failed", log.Err(err))
			}
		}
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, intent *NodeIntent) error {

	return nil
}
