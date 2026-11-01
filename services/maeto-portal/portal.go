package maetoportal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/saphalpdyl/maeto/libs/controlapi"
	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/nodesync"
	"github.com/saphalpdyl/maeto/libs/swan"
	"github.com/saphalpdyl/maeto/services/maeto-portal/log"
)

type Portal struct {
	InstanceId string

	js jetstream.JetStream

	// Intents pushed to by the intentkv watcher and read by the Reconciler
	intentFeed chan *nodesync.NodeIntent
	reconciler *dataplane.Reconciler

	swanClient *swan.Client

	config PortalConfig
	logger *slog.Logger
}

func NewPortal(
	instanceId string,
	js jetstream.JetStream,
	config PortalConfig,
	dp dataplane.Dataplane,
	logger *slog.Logger,
	swanClient *swan.Client,
) *Portal {
	intentFeed := make(chan *nodesync.NodeIntent, 32)
	reconciler := dataplane.NewReconciler(
		dp,
		nodesync.NodeTypeCPE,
		logger.With(log.Domain(log.DomainReconciler)),
		intentFeed,
	)

	return &Portal{
		InstanceId: instanceId,
		config:     config,
		logger:     logger,
		js:         js,
		reconciler: reconciler,
		intentFeed: intentFeed,
		swanClient: swanClient,
	}
}

func (p *Portal) Run(ctx context.Context) error {
	p.logger.InfoContext(ctx, "portal starting", log.PortalID(p.config.PortalID))

	if !p.waitForReady(ctx) {
		p.logger.ErrorContext(ctx, "portal failed to become ready")
		return nil
	}

	p.logger.InfoContext(ctx, "portal ready", log.PortalID(p.config.PortalID))

	eventChan, err := p.swanClient.WatchIKEUpdownEvents(ctx)
	if err != nil {
		p.logger.ErrorContext(ctx, "portal swan watch events failed", log.PortalID(p.config.PortalID))
		return err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-eventChan:
				if len(e.IKE.ChildSAs) != 1 {
					p.logger.WarnContext(ctx, fmt.Sprintf("child-updown: expected 1 child SA, got %d", len(e.IKE.ChildSAs)))
					continue
				}

				var childSA swan.ChildSA
				for _, v := range e.IKE.ChildSAs {
					childSA = v
				}

				if e.IKE.State != "ESTABLISHED" || childSA.State != "INSTALLED" {
					p.logger.ErrorContext(ctx, "child-updown: invalid state", log.PortalID(p.config.PortalID))
				}

				ifID, err := childSA.IfID()
				if err != nil {
					p.logger.ErrorContext(ctx, "failed to parse IfID into uint32", log.Err(err))
					continue
				}

				if err := p.sendCPETunnelUpdate(ifID, ctx); err != nil {
					p.logger.ErrorContext(ctx, "failed to send CPETunnelUpdate", log.Err(err))
					continue
				}

				p.logger.InfoContext(ctx, "cpe tunnel update successful",
					log.PortalID(p.config.PortalID),
					slog.Int("if_id", int(ifID)),
				)
			}
		}
	}()

	swanConnOptsReq := controlapi.PortalAuthEndpointRequest{
		PortalID: p.config.PortalID,
	}
	swanConnOptsReqData, err := json.Marshal(swanConnOptsReq)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to marshal portal auth request", log.Err(err))
		return err
	}

	data, err := p.js.Conn().Request(controlapi.SubjectPortalAuthIdentity, swanConnOptsReqData, 3*time.Second)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to request portal auth identity", log.Err(err))
		return err
	}

	var swanConnOptsResp controlapi.PortalAuthEndpointResponse
	if err := json.Unmarshal(data.Data, &swanConnOptsResp); err != nil {
		p.logger.ErrorContext(ctx, "failed to unmarshal portal auth response", log.Err(err))
		return err
	}

	p.logger.InfoContext(ctx, "received portal auth response",
		log.PortalID(p.config.PortalID),
	)

	// TODO: Generate credentials in the control plane signed by the CA
	if err := p.loadCredentials(ctx); err != nil {
		return err
	}

	if err := p.loadConnection(ctx, swanConnOptsResp.AttachNodeAddr,
		swanConnOptsResp.LocalSwanIdentity, swanConnOptsResp.RemoteSwanIdentity); err != nil {
		return err
	}

	if err := p.swanClient.InitiateTunnel(ctx, "pop"); err != nil {
		return err
	}

	// Handles the RESYNC event when the controller restarts
	go p.setupResyncWatch(ctx)

	go func() {
		err := p.reconciler.Start(ctx)
		if err != nil {
			p.logger.ErrorContext(ctx, "reconciler failed to start", log.Err(err))
		}
	}()

	go func() {
		err := nodesync.Watch(
			ctx,
			p.js,
			p.logger.With(log.Domain(log.DomainControlPlane)),
			nodesync.IntentBucket,
			nodesync.Key(nodesync.PrefixCPE, p.config.PortalID, nodesync.LaneIntent),
			p.intentFeed,
		)

		if err != nil {
			p.logger.ErrorContext(ctx, "intent watch failed",
				log.Domain(log.DomainControlPlane),
				log.Err(err),
			)
		}
	}()

	<-ctx.Done()

	return p.shutdown(ctx)
}

func (p *Portal) setupResyncWatch(ctx context.Context) {
	_, _ = p.js.Conn().Subscribe(controlapi.SubjectControllerCPETunnelResync, func(msg *nats.Msg) {
		sas, err := p.swanClient.GetSAs(ctx)
		if err != nil {
			p.logger.ErrorContext(ctx, "failed to get SAS info", log.Err(err))
		}

		// Loop through the list of IKE_SA ( for portal it's supposed to be one)
		for _, ikeSA := range sas {
			var ifID uint32

			for _, childSA := range ikeSA.ChildSAs {
				if ikeSA.State != "ESTABLISHED" || childSA.State != "INSTALLED" {
					p.logger.ErrorContext(ctx, "child-updown: invalid state", log.PortalID(p.config.PortalID))
					continue
				}

				childIfID, err := childSA.IfID()
				if err != nil {
					p.logger.ErrorContext(ctx, "failed to parse IfID into uint32", log.Err(err))
					continue
				}

				ifID = childIfID
			}

			if ifID == 0 {
				continue
			}

			err := p.sendCPETunnelUpdate(ifID, ctx)
			if err != nil {
				p.logger.ErrorContext(ctx, "failed to send CPETunnelUpdate", log.Err(err))
				continue
			}

		}
		if err := msg.Ack(); err != nil {
			p.logger.ErrorContext(ctx, "failed to ack message", log.Err(err))
		}
	})

	<-ctx.Done()
}

func (p *Portal) sendCPETunnelUpdate(ifID uint32, ctx context.Context) error {
	req := controlapi.CPETunnelUpdateRequest{
		PortalID: p.config.PortalID,
		IfID:     ifID,
	}

	data, err := json.Marshal(req)
	if err != nil {
		p.logger.ErrorContext(ctx, "failed to marshal cpe tunnel update request", log.Err(err))
		return err
	}

	respData, err := p.js.Conn().Request(controlapi.SubjectCPETunnelUpdate, data, 5*time.Second)

	if err != nil {
		p.logger.ErrorContext(ctx, "failed to publish cpe tunnel update request", log.Err(err))
		return err
	}

	p.logger.InfoContext(ctx, "cpe tunnel update request published",
		log.PortalID(p.config.PortalID),
		slog.Int("if_id", int(ifID)),
	)

	var resp controlapi.TunnelUpdateResponse
	if err := json.Unmarshal(respData.Data, &resp); err != nil {
		p.logger.ErrorContext(ctx, "failed to unmarshal cpe tunnel update response", log.Err(err))
		return err
	}

	if !resp.Ok {
		p.logger.ErrorContext(ctx, "cpe tunnel update failed")
		return err
	}
	return nil
}

func (p *Portal) shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.config.ShutdownGracePeriod)
	defer cancel()

	p.logger.InfoContext(ctx, "portal shutting down",
		log.DurationMs(p.config.ShutdownGracePeriod.Milliseconds()),
	)

	return nil
}

func (p *Portal) waitForReady(ctx context.Context) bool {
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		attempt++

		data, err := p.js.Conn().Request(controlapi.SubjectHealthReady, nil, time.Second)
		if err != nil {
			p.logger.WarnContext(ctx, "control plane unreachable",
				log.Domain(log.DomainControlPlane),
				log.Attempt(attempt),
				log.Err(err),
			)
		} else {
			var resp struct {
				Ready string `json:"ready"`
			}

			if err = json.Unmarshal(data.Data, &resp); err != nil {
				p.logger.ErrorContext(ctx, "failed to parse health response",
					log.Domain(log.DomainControlPlane),
					log.Err(err),
				)
			} else if resp.Ready == "true" {
				return true
			}
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(2000 * time.Millisecond):
		}
	}
}
