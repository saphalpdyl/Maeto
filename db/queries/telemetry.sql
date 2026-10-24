-- name: InsertIfaceCounters :exec
INSERT INTO iface_counters (time, node_id, iface, rx_bytes, tx_bytes, rx_packets, tx_packets, rx_dropped, tx_dropped)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (node_id, iface, time) DO NOTHING;

-- name: ListIfaceCounters :many
SELECT time, node_id, iface, rx_bytes, tx_bytes, rx_packets, tx_packets, rx_dropped, tx_dropped
FROM iface_counters
WHERE node_id = $1
  AND iface = $2
  AND time >= $3
ORDER BY time;

-- name: ListNodeIfaceCounters :many
SELECT time, node_id, iface, rx_bytes, tx_bytes, rx_packets, tx_packets, rx_dropped, tx_dropped
FROM iface_counters
WHERE node_id = $1
  AND time >= $2
ORDER BY iface, time;

-- name: InsertLinkSample :exec
INSERT INTO link_samples (time, telemetry_key, sequence, latency_ms, lost)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (telemetry_key, time) DO NOTHING;

-- name: ListLinkSamples :many
SELECT time, telemetry_key, sequence, latency_ms, lost
FROM link_samples
WHERE telemetry_key = $1
  AND time >= $2
ORDER BY time;

-- name: ListLatestLinkSamples :many
SELECT time, telemetry_key, sequence, latency_ms, lost
FROM link_samples
WHERE telemetry_key = $1
ORDER BY time DESC
LIMIT $2;
