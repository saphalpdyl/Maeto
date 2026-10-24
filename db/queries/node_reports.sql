-- name: InsertNodeReport :exec
INSERT INTO node_reports (time, node_id, node_type, generation, observed, converged, passes, error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (node_id, time) DO NOTHING;

-- name: ListNodeReports :many
SELECT time, node_id, node_type, generation, observed, converged, passes, error
FROM node_reports
WHERE node_id = $1
  AND time >= $2
ORDER BY time DESC;

-- name: ListLatestNodeReports :many
SELECT time, node_id, node_type, generation, observed, converged, passes, error
FROM node_reports
WHERE node_id = $1
ORDER BY time DESC
LIMIT $2;
