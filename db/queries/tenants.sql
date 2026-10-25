-- name: GetTenant :one
SELECT id, allocation, tier, tier_index, vrf_table
FROM tenants
WHERE id = $1;

-- name: ListTenants :many
SELECT id, allocation, tier, tier_index, vrf_table
FROM tenants
ORDER BY id;

-- name: CreateTenant :exec
INSERT INTO tenants (id, allocation, tier, tier_index, vrf_table)
VALUES ($1, $2, $3, $4, $5);

-- name: UpdateTenant :exec
UPDATE tenants
SET allocation = $2,
    tier = $3,
    tier_index = $4,
    vrf_table = $5
WHERE id = $1;

-- name: DeleteTenant :exec
DELETE FROM tenants
WHERE id = $1;

-- name: DeleteAllTenants :exec
DELETE FROM tenants;

-- name: GetSite :one
SELECT portal_id, tenant_id, cpe, node, prefix, attach, attach_node, if_id, identity, reservation_mbps
FROM sites
WHERE portal_id = $1;

-- name: GetSiteByIdentity :one
SELECT portal_id, tenant_id, cpe, node, prefix, attach, attach_node, if_id, identity, reservation_mbps
FROM sites
WHERE identity = $1;

-- name: ListSites :many
SELECT portal_id, tenant_id, cpe, node, prefix, attach, attach_node, if_id, identity, reservation_mbps
FROM sites
ORDER BY tenant_id, if_id;

-- name: ListSitesByTenant :many
SELECT portal_id, tenant_id, cpe, node, prefix, attach, attach_node, if_id, identity, reservation_mbps
FROM sites
WHERE tenant_id = $1
ORDER BY if_id;

-- name: ListSitesByAttach :many
SELECT portal_id, tenant_id, cpe, node, prefix, attach, attach_node, if_id, identity, reservation_mbps
FROM sites
WHERE attach = $1
ORDER BY if_id;

-- name: CreateSite :exec
INSERT INTO sites (portal_id, tenant_id, cpe, node, prefix, attach, attach_node, if_id, identity, reservation_mbps)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: UpdateSite :exec
UPDATE sites
SET tenant_id = $2,
    cpe = $3,
    node = $4,
    prefix = $5,
    attach = $6,
    attach_node = $7,
    if_id = $8,
    identity = $9,
    reservation_mbps = $10
WHERE portal_id = $1;

-- name: DeleteSite :exec
DELETE FROM sites
WHERE portal_id = $1;

-- name: GetTrafficClass :one
SELECT label, name, class_index
FROM traffic_classes
WHERE label = $1;

-- name: ListTrafficClasses :many
SELECT label, name, class_index
FROM traffic_classes
ORDER BY class_index;

-- name: CreateTrafficClass :exec
INSERT INTO traffic_classes (label, name, class_index)
VALUES ($1, $2, $3);

-- name: UpdateTrafficClass :exec
UPDATE traffic_classes
SET name = $2,
    class_index = $3
WHERE label = $1;

-- name: DeleteTrafficClass :exec
DELETE FROM traffic_classes
WHERE label = $1;

-- name: DeleteAllTrafficClasses :exec
DELETE FROM traffic_classes;

-- name: ListTrafficClassDSCP :many
SELECT label, dscp
FROM traffic_class_dscp
WHERE label = $1
ORDER BY dscp;

-- name: AddTrafficClassDSCP :exec
INSERT INTO traffic_class_dscp (label, dscp)
VALUES ($1, $2);

-- name: DeleteTrafficClassDSCP :exec
DELETE FROM traffic_class_dscp
WHERE label = $1;

-- name: ListTrafficClassObjectives :many
SELECT label, dimension, weight
FROM traffic_class_objectives
WHERE label = $1
ORDER BY dimension;

-- name: SetTrafficClassObjective :exec
INSERT INTO traffic_class_objectives (label, dimension, weight)
VALUES ($1, $2, $3)
ON CONFLICT (label, dimension) DO UPDATE
SET weight = EXCLUDED.weight;

-- name: DeleteTrafficClassObjectives :exec
DELETE FROM traffic_class_objectives
WHERE label = $1;

-- name: ListSiteClassReservations :many
SELECT portal_id, label, mbps
FROM site_class_reservations
WHERE portal_id = $1
ORDER BY label;

-- name: SetSiteClassReservation :exec
INSERT INTO site_class_reservations (portal_id, label, mbps)
VALUES ($1, $2, $3)
ON CONFLICT (portal_id, label) DO UPDATE
SET mbps = EXCLUDED.mbps;

-- name: DeleteSiteClassReservations :exec
DELETE FROM site_class_reservations
WHERE portal_id = $1;
