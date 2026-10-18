package controlplane

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/saphalpdyl/maeto/libs/dataplane"
)

const MaxSIDCollisionChecks = 5

func withHextets(base netip.Prefix, offsetBits int, funcID uint16) netip.Addr {
	b := base.Addr().As16()
	offset := offsetBits / 8

	binary.BigEndian.PutUint16(b[offset:offset+2], funcID)

	return netip.AddrFrom16(b)
}

// Call under lock
func (r *ServiceRegistry) getOrGenerateSID(locatorPrefix netip.Prefix, tenantID string, sidType dataplane.EncapType) (netip.Addr, error) {
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

		allocated, exists := r.sidAllocationMap[sid]
		if !exists || !allocated {
			r.sidCursor++
			r.sidTenantMap[tenantMapKey] = sid
			r.sidAllocationMap[sid] = true
			return sid, nil
		}

		r.logger.Info("SID Collision: already is allocated", slog.String("sid", sid.String()))
		r.sidCursor++
		// TODO: uint16 might get exhausted
	}

	return netip.Addr{}, fmt.Errorf("couldn't generate SID: too many collisions, tried %d times", MaxSIDCollisionChecks)

}

func (r *ServiceRegistry) GetOrGenerateSID(locatorPrefix netip.Prefix, tenantID string, sidType dataplane.EncapType) (netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.getOrGenerateSID(locatorPrefix, tenantID, sidType)
}
