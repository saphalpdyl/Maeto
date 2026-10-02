from dataclasses import dataclass, field


@dataclass(frozen=True)
class Defaults:
    locator_prefix: str
    link_prefix: str
    edge_prefix: str


@dataclass
class Pop:
    id: str
    index: int          # stable identity, drives locator + isis id + derived link/edge ips
    node_name: str      # PopA
    clab_label: str
    data: dict = field(default_factory=dict)


@dataclass
class Tenant:
    id: int
    allocation: str     # every site prefix must sit inside this
    tier: str           # service class, uninterpreted here
    data: dict = field(default_factory=dict)


@dataclass
class Cpe:
    id: str
    node_name: str      # CpeA
    clab_label: str
    attach: str         # pop id
    tenant: int
    prefix: str         # this site's lan
    portal_id: str # LDevID equivalent
    reservation_mbps: float   # bandwidth this site has reserved
    data: dict = field(default_factory=dict)


@dataclass
class CoreLink:
    index: int          # stable subnet index derived from endpoints + instance
    a: str              # pop id, lower index (gets ::1)
    b: str              # pop id, higher index (gets ::2)
    instance: int       # 1-based redundancy instance
    bandwidth_mbps: float     # nominal capacity, assigned per parallel link
    delay_ms: float           # one-way propagation delay, stands in for distance
    start_jitter: float       # 0..1, spread applied to bandwidth at generate time


@dataclass
class Topology:
    name: str
    defaults: Defaults
    pops: list          # [Pop]
    tenants: list     # [Tenant]
    cpes: list          # [Cpe]
    links: list         # [CoreLink]

    def pop_by_id(self, pid):
        return self._pops_by_id[pid]

    def tenant_by_id(self, cid):
        return self._tenants_by_id[cid]

    def __post_init__(self):
        self._pops_by_id = {p.id: p for p in self.pops}
        self._tenants_by_id = {c.id: c for c in self.tenants}
