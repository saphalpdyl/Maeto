CREATE TABLE tenants (
    id         INTEGER PRIMARY KEY,
    allocation CIDR    NOT NULL,
    tier       TEXT    NOT NULL,
    tier_index INTEGER NOT NULL,
    vrf_table  INTEGER NOT NULL
);

CREATE TABLE sites (
    portal_id        TEXT             PRIMARY KEY,
    tenant_id        INTEGER          NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    cpe              TEXT             NOT NULL,
    node             TEXT             NOT NULL,
    prefix           CIDR             NOT NULL,
    attach           TEXT             NOT NULL,
    attach_node      TEXT             NOT NULL,
    if_id            BIGINT           NOT NULL,
    identity         TEXT             NOT NULL UNIQUE,
    reservation_mbps DOUBLE PRECISION NOT NULL DEFAULT 0
);

CREATE INDEX sites_tenant_id ON sites (tenant_id);
CREATE INDEX sites_attach ON sites (attach);

CREATE TABLE traffic_classes (
    label       BIGINT  PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,
    class_index INTEGER NOT NULL
);

CREATE TABLE traffic_class_dscp (
    label BIGINT   NOT NULL REFERENCES traffic_classes (label) ON DELETE CASCADE,
    dscp  SMALLINT NOT NULL CHECK (dscp BETWEEN 0 AND 63),
    PRIMARY KEY (label, dscp)
);

CREATE TABLE traffic_class_objectives (
    label     BIGINT           NOT NULL REFERENCES traffic_classes (label) ON DELETE CASCADE,
    dimension TEXT             NOT NULL,
    weight    DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (label, dimension)
);

CREATE TABLE site_class_reservations (
    portal_id TEXT             NOT NULL REFERENCES sites (portal_id) ON DELETE CASCADE,
    label     BIGINT           NOT NULL REFERENCES traffic_classes (label) ON DELETE CASCADE,
    mbps      DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (portal_id, label)
);
