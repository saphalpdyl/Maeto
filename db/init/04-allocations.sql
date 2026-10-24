CREATE TABLE sid_allocations (
    sid       INET PRIMARY KEY,
    locator   CIDR NOT NULL,
    tenant_id TEXT NOT NULL,
    sid_type  TEXT NOT NULL,
    UNIQUE (locator, tenant_id, sid_type)
);
