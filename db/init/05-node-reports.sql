CREATE TABLE node_reports (
    time       TIMESTAMPTZ NOT NULL,
    node_id    TEXT        NOT NULL,
    node_type  TEXT        NOT NULL,
    generation BIGINT      NOT NULL,
    observed   BOOLEAN     NOT NULL,
    converged  BOOLEAN     NOT NULL,
    passes     INTEGER     NOT NULL,
    error      TEXT        NOT NULL DEFAULT '',
    PRIMARY KEY (node_id, time)
);

SELECT create_hypertable('node_reports', 'time');

SELECT add_retention_policy('node_reports', INTERVAL '7 days');
