package maetoagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/saphalpdyl/maeto/libs/controlapi"
	"github.com/saphalpdyl/maeto/libs/swan"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

func (a *Agent) watchTunnelUpdates(ctx context.Context, eventChan <-chan *swan.UpDownEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-eventChan:
			if e.Event != "child-updown" {
				continue
			}

			if len(e.IKE.ChildSAs) != 1 {
				a.logger.WarnContext(ctx, fmt.Sprintf("child-updown: expected 1 child SA, got %d", len(e.IKE.ChildSAs)))
				continue
			}

			err := a.sendPETunnelUpdate(ctx, e.IKE)
			if err != nil {
				a.logger.ErrorContext(ctx, fmt.Sprintf("child-updown: failed to send PETunnel update: %v", err))
				continue
			}

			a.logger.InfoContext(ctx, "successfully sent req")
		}
	}
}

func (a *Agent) sendPETunnelUpdate(
	ctx context.Context,
	ikeSA swan.IKESA,
) error {
	var childSA swan.ChildSA
	for _, v := range ikeSA.ChildSAs {
		childSA = v
	}

	if ikeSA.State != "ESTABLISHED" || childSA.State != "INSTALLED" {
		return fmt.Errorf("invalid state IKE_SA(%s) and CHILD_SA(%s)", ikeSA.State, childSA.State)
	}

	ifID, err := childSA.IfID()
	if err != nil {
		return fmt.Errorf("invalid child sa if_id: %w", err)
	}

	portalID, found := strings.CutSuffix(ikeSA.RemoteID, ".cpe.maeto.net")
	if !found || portalID == "" || strings.Contains(portalID, ".") {
		return fmt.Errorf("remote-id %q is not <portal_id>.cpe.maeto.net", ikeSA.RemoteID)
	}

	// Push the connection event to the control plane
	// 	The control plane then pushes to ServiceRegistry
	// 	and delivers a new NodeIntent to reconcile on.
	// So connection -> push_to_controller -> new intent -> reconcile -> FIB updated
	var req controlapi.PETunnelUpdateRequest
	req.PortalID = portalID
	req.IfID = ifID
	req.NodeID = a.node.ID

	data, err := json.Marshal(req)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to marshal request", log.Err(err))
		return err
	}

	resp, err := a.js.Conn().Request(controlapi.SubjectPETunnelUpdate, data, 5*time.Second)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to send push tunnel initiate request", log.Err(err))
		return err
	}

	var pushResp controlapi.TunnelUpdateResponse
	if err := json.Unmarshal(resp.Data, &pushResp); err != nil {
		a.logger.ErrorContext(ctx, "failed to unmarshal push tunnel initiate response", log.Err(err))
		return err
	}

	if !pushResp.Ok {
		a.logger.ErrorContext(ctx, "push tunnel initiate request failed", slog.String("portal_id", portalID))
		return err
	}
	return nil
}
