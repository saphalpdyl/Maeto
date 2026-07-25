from dataclasses import dataclass, field


@dataclass(frozen=True)
class Defaults:
    locator_prefix: str
    link_prefix: str
    host_prefix: str


@dataclass
class Pop:
    id: str
    index: int          # stable identity, drives locator + isis id + derived link/host ips
    node_name: str      # PopA
    clab_label: str
    data: dict = field(default_factory=dict)


@dataclass
class Host:
    id: str
    node_name: str      # HostA
    clab_label: str
    attach: str         # pop id
    data: dict = field(default_factory=dict)


@dataclass
class CoreLink:
    index: int          # stable subnet index derived from endpoints + instance
    a: str              # pop id, lower index (gets ::1)
    b: str              # pop id, higher index (gets ::2)
    instance: int       # 1-based redundancy instance


@dataclass
class Topology:
    name: str
    defaults: Defaults
    pops: list          # [Pop]
    hosts: list         # [Host]
    links: list         # [CoreLink]

    def pop_by_id(self, pid):
        return self._pops_by_id[pid]

    def __post_init__(self):
        self._pops_by_id = {p.id: p for p in self.pops}
