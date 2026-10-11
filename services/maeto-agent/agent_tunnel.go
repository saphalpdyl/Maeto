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
	"github.com/strongswan/govici/vici"
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

			var cSa swan.ChildSA
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

func (a *Agent) watchEvents(ctx context.Context, s *vici.Session) (<-chan *swan.UpDownEvent, error) {
	viciEventChan := make(chan vici.Event, 16)
	s.NotifyEvents(viciEventChan)

	eventChan := make(chan *swan.UpDownEvent, 32)

	if err := s.Subscribe("ike-updown", "child-updown"); err != nil {
		a.logger.WarnContext(ctx, "ike-updown EVENT failed to subscribe", log.Err(err))
		return nil, err
	}

	go func() {
		defer s.StopEvents(viciEventChan)

		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-viciEventChan:
				if !ok {
					a.logger.InfoContext(ctx, "ike/child even not ok")
					continue
				}

				parsedEvent, err := swan.ParseUpDownEvent(e.Name, e.Message)
				if err != nil {
					a.logger.ErrorContext(ctx, "failed to parse vici updown event", log.Err(err))
					continue
				}

				eventChan <- parsedEvent
			}
		}
	}()

	return eventChan, nil
}
