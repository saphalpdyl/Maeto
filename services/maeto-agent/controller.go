// Contains methods of interactions with the controller
package maetoagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/saphalpdyl/maeto/libs/controlapi"
	"github.com/saphalpdyl/maeto/services/maeto-agent/log"
)

func (a *Agent) watchTunnelUpdates(ctx context.Context, eventChan <-chan *UpDownEvent) {
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

			var cSa ChildSA
			for _, v := range e.IKE.ChildSAs {
				cSa = v
			}

			// X.509 cert of CPE to get SAN for portalID
			cert, err := e.RemoteCertificate()
			if err != nil {
				a.logger.ErrorContext(ctx, "failed to get remote certificate", log.Err(err))
				continue
			}

			ifID, err := cSa.IfID()
			if err != nil {
				a.logger.ErrorContext(ctx, "invalid child sa if_id", log.Err(err))
				continue
			}

			// Assume a <portal_id>.cpe.maeto.net format
			// *.cpe.maeto.net is already verified by strongswan, so we can trust the SAN
			cpeSAN := cert.DNSNames[0]
			sanParts := strings.Split(cpeSAN, ".")
			if len(sanParts) <= 0 {
				a.logger.ErrorContext(ctx, "invalid SAN format", slog.String("san", cpeSAN))
				continue
			}

			portalID := sanParts[0]

			a.logger.InfoContext(ctx, "got child updown event SAN", slog.Any("san", cpeSAN), slog.Any("if_id", ifID), slog.Any("portal_id", portalID))

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
				continue
			}

			resp, err := a.js.Conn().Request(controlapi.SubjectPETunnelUpdate, data, 5*time.Second)
			if err != nil {
				a.logger.ErrorContext(ctx, "failed to send push tunnel initiate request", log.Err(err))
				continue
			}

			var pushResp controlapi.TunnelUpdateResponse
			if err := json.Unmarshal(resp.Data, &pushResp); err != nil {
				a.logger.ErrorContext(ctx, "failed to unmarshal push tunnel initiate response", log.Err(err))
				continue
			}

			if !pushResp.Ok {
				a.logger.ErrorContext(ctx, "push tunnel initiate request failed", slog.String("portal_id", portalID))
				continue
			}

			a.logger.InfoContext(ctx, "successfully sent req", slog.Any("data", req))
		}
	}
}

// Waits for the control plane to reach healthy status
func (a *Agent) waitForReady(ctx context.Context) bool {
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		attempt++

		data, err := a.js.Conn().Request(controlapi.SubjectHealthReady, nil, time.Second)
		if err != nil {
			a.logger.WarnContext(ctx, "control plane unreachable",
				log.Domain(log.DomainControlPlane),
				log.Attempt(attempt),
				log.Err(err),
			)
		} else {
			var resp struct {
				Ready string `json:"ready"`
			}

			if err = json.Unmarshal(data.Data, &resp); err != nil {
				a.logger.ErrorContext(ctx, "failed to parse health response",
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
