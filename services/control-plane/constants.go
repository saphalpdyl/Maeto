package controlplane

import "time"

const LATEST_STATE_FILENAME string = "latest.json"
const TOPOLOGY_DATA_FILENAME string = "topology.data.json"
const TENANT_DB_FILENAME string = "tenant.db.json"

const ProbeResultMaxAge time.Duration = 3 * time.Hour
