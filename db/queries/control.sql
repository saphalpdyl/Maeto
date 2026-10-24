-- name: GetControlState :one
SELECT id, schema_version, fingerprint, sid_cursor, booted_at
FROM control_state
WHERE id = 1;

-- name: SetFingerprint :exec
UPDATE control_state
SET fingerprint = $1
WHERE id = 1;

-- name: SetSIDCursor :exec
UPDATE control_state
SET sid_cursor = $1
WHERE id = 1;

-- name: SetBootedAt :exec
UPDATE control_state
SET booted_at = $1
WHERE id = 1;

-- name: GetNodeGeneration :one
SELECT node_id, version, generation
FROM node_generations
WHERE node_id = $1;

-- name: ListNodeGenerations :many
SELECT node_id, version, generation
FROM node_generations
ORDER BY node_id;

-- name: UpsertNodeGeneration :exec
INSERT INTO node_generations (node_id, version, generation)
VALUES ($1, $2, $3)
ON CONFLICT (node_id) DO UPDATE
SET version = EXCLUDED.version,
    generation = EXCLUDED.generation;

-- name: DeleteNodeGeneration :exec
DELETE FROM node_generations
WHERE node_id = $1;
