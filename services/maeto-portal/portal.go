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
				// treat even as trigger and send all the tunnels we have
				var down uint32
				if !e.Up {
					down = e.IKE.UniqueID
				}

				if err := p.sendAllTunnels(ctx, down); err != nil {
					p.logger.ErrorContext(ctx, "failed to send CPETunnelUpdate", log.Err(err))
					continue
				}

				p.logger.InfoContext(ctx, "cpe tunnel update successful", log.PortalID(p.config.PortalID))
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

	for nodeID, attach := range swanConnOptsResp.Attachments {
		confName := fmt.Sprintf("pop%s", attach.NodeID)

		if err := p.loadConnection(
			ctx,
			attach.AttachAddr,
			swanConnOptsResp.LocalSwanIdentity,
			attach.RemoteSwanIdentity,
			confName,
		); err != nil {
			p.logger.ErrorContext(ctx, fmt.Sprintf("failed to load connection for %s", nodeID), log.Err(err))
			continue
		}

		if err := p.swanClient.InitiateTunnel(ctx, confName); err != nil {
			p.logger.ErrorContext(ctx, fmt.Sprintf("failed to initiate tunnel for %s", nodeID), log.Err(err))
		}
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
		if err := p.sendAllTunnels(ctx, 0); err != nil {
			p.logger.ErrorContext(ctx, "failed to send CPETunnelUpdate", log.Err(err))
		}

		if err := msg.Ack(); err != nil {
			p.logger.ErrorContext(ctx, "failed to ack message", log.Err(err))
		}
	})

	<-ctx.Done()
}

func (p *Portal) sendAllTunnels(ctx context.Context, down uint32) error {
	sas, err := p.swanClient.GetSAs(ctx)
	if err != nil {
		return fmt.Errorf("list SAs: %w", err)
	}

	ifIDs := make(map[string]uint32)
	for _, ikeSA := range sas {
		if ikeSA.State != "ESTABLISHED" || (down != 0 && ikeSA.UniqueID == down) {
			continue
		}

		for _, childSA := range ikeSA.ChildSAs {
			if childSA.State != "INSTALLED" {
				continue
			}

			ifID, err := childSA.IfID()
			if err != nil {
				p.logger.ErrorContext(ctx, "failed to parse IfID into uint32", log.Err(err))
				continue
			}

			ifIDs[ikeSA.RemoteID] = ifID
		}
	}

	return p.sendCPETunnelUpdate(ifIDs, ctx)
}

func (p *Portal) sendCPETunnelUpdate(ifIDs map[string]uint32, ctx context.Context) error {
	req := controlapi.CPETunnelUpdateRequest{
		PortalID: p.config.PortalID,
		IfIds:    ifIDs,
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
		slog.Int("tunnels", len(ifIDs)),
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
