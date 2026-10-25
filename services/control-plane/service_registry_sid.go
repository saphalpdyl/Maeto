package controlplane

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/saphalpdyl/maeto/libs/dataplane"
	"github.com/saphalpdyl/maeto/libs/store"
	"github.com/saphalpdyl/maeto/services/control-plane/log"
)

const MaxSIDCollisionChecks = 5

func withHextets(base netip.Prefix, offsetBits int, funcID uint16) netip.Addr {
	b := base.Addr().As16()
	offset := offsetBits / 8

	binary.BigEndian.PutUint16(b[offset:offset+2], funcID)

	return netip.AddrFrom16(b)
}

// Call under lock
func (r *ServiceRegistry) getOrGenerateSID(ctx context.Context, locatorPrefix netip.Prefix, tenantID string, sidType dataplane.EncapType) (netip.Addr, error) {
	// We don't want sticky ID across PEs
	tenantMapKey := fmt.Sprintf("%s.%s.%s", locatorPrefix.Addr().String(), tenantID, string(sidType))
	existingSID, exists := r.sidTenantMap[tenantMapKey]
	if exists {
		r.sidAllocationMap[existingSID] = true
		return existingSID, nil
	}

	for range MaxSIDCollisionChecks {
		funcID := permute(r.sidCursor)
		sid := withHextets(locatorPrefix, 48, funcID)

		allocated := r.sidAllocationMap[sid]
		if allocated {
			r.logger.InfoContext(ctx, "SID Collision: already is allocated", slog.String("sid", sid.String()))
			r.sidCursor++
			continue
		}

		// Try db-write before in-mem write
		err := r.db.InTx(ctx, func(q *store.Queries) error {
			err := q.CreateSIDAllocation(ctx, store.CreateSIDAllocationParams{
				Sid:      sid,
				Locator:  locatorPrefix,
				TenantID: tenantID,
				SidType:  string(sidType),
			})
			if err != nil {
				r.logger.ErrorContext(ctx, "db: couldn't write SID allocation", slog.String("sid", sid.String()), log.Err(err))
				return err
			}

			err = q.SetSIDCursor(ctx, int32(r.sidCursor+1))
			if err != nil {
				r.logger.ErrorContext(ctx, "db: couldn't set SID cursor", log.Err(err))
				return err
			}

			return nil
		})

		if err != nil {
			return netip.Addr{}, err
		}

		r.sidCursor++
		r.sidTenantMap[tenantMapKey] = sid
		r.sidAllocationMap[sid] = true

		return sid, nil
	}

	return netip.Addr{}, fmt.Errorf("couldn't generate SID: too many collisions, tried %d times", MaxSIDCollisionChecks)

}

func (r *ServiceRegistry) GetOrGenerateSID(ctx context.Context, locatorPrefix netip.Prefix, tenantID string, sidType dataplane.EncapType) (netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.getOrGenerateSID(ctx, locatorPrefix, tenantID, sidType)
}
