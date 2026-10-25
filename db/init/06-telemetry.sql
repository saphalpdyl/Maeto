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
