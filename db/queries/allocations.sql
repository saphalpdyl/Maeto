-- name: GetSIDAllocation :one
SELECT sid, locator, tenant_id, sid_type
FROM sid_allocations
WHERE locator = $1
  AND tenant_id = $2
  AND sid_type = $3;

-- name: ListSIDAllocations :many
SELECT sid, locator, tenant_id, sid_type
FROM sid_allocations
ORDER BY sid;

-- name: CreateSIDAllocation :exec
INSERT INTO sid_allocations (sid, locator, tenant_id, sid_type)
VALUES ($1, $2, $3, $4);

-- name: DeleteSIDAllocation :exec
DELETE FROM sid_allocations
WHERE sid = $1;

-- name: DeleteAllSIDAllocations :exec
DELETE FROM sid_allocations;
