CREATE TABLE control_state (
    id             SMALLINT    PRIMARY KEY CHECK (id = 1),
    schema_version INTEGER     NOT NULL,
    fingerprint    TEXT        NOT NULL DEFAULT '',
    sid_cursor     INTEGER     NOT NULL DEFAULT 0,
    booted_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO control_state (id, schema_version) VALUES (1, 1);

CREATE TABLE node_generations (
    node_id    TEXT   PRIMARY KEY,
    version    BIGINT NOT NULL,
    generation BIGINT NOT NULL
);
