CREATE TABLE iface_counters (
    time       TIMESTAMPTZ NOT NULL,
    node_id    TEXT        NOT NULL,
    iface      TEXT        NOT NULL,
    rx_bytes   BIGINT      NOT NULL,
    tx_bytes   BIGINT      NOT NULL,
    rx_packets BIGINT      NOT NULL,
    tx_packets BIGINT      NOT NULL,
    rx_dropped BIGINT      NOT NULL,
    tx_dropped BIGINT      NOT NULL,
    PRIMARY KEY (node_id, iface, time)
);

SELECT create_hypertable('iface_counters', 'time');

SELECT add_retention_policy('iface_counters', INTERVAL '7 days');

CREATE TABLE link_samples (
    time          TIMESTAMPTZ      NOT NULL,
    telemetry_key TEXT             NOT NULL,
    sequence      BIGINT           NOT NULL,
    latency_ms    DOUBLE PRECISION,
    PRIMARY KEY (telemetry_key, time)
);

SELECT create_hypertable('link_samples', 'time');

SELECT add_retention_policy('link_samples', INTERVAL '7 days');

CREATE TABLE node_heartbeats (
    time    TIMESTAMPTZ NOT NULL,
    node_id TEXT        NOT NULL,
    ready   BOOLEAN     NOT NULL,
    PRIMARY KEY (node_id, time)
);

SELECT create_hypertable('node_heartbeats', 'time');

SELECT add_retention_policy('node_heartbeats', INTERVAL '7 days');

CREATE TABLE path_changes (
    time       TIMESTAMPTZ      NOT NULL,
    tenant     BIGINT           NOT NULL,
    label      BIGINT           NOT NULL,
    src        TEXT             NOT NULL,
    dst        TEXT             NOT NULL,
    kind       TEXT             NOT NULL,
    from_nodes TEXT[]           NOT NULL,
    to_nodes   TEXT[]           NOT NULL,
    from_cost  DOUBLE PRECISION,
    to_cost    DOUBLE PRECISION,
    causes     JSONB            NOT NULL DEFAULT '[]',
    PRIMARY KEY (tenant, label, src, dst, time)
);

SELECT create_hypertable('path_changes', 'time');

SELECT add_retention_policy('path_changes', INTERVAL '7 days');
