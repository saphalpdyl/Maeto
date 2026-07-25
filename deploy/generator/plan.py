from dataclasses import dataclass

from . import addressing


@dataclass
class Interface:
    name: str           # eth1
    role: str           # core | host
    peer: str           # peer node name
    address: str        # ::1/64
    description: str


@dataclass
class Endpoint:
    node: str
    iface: str
    address: str


@dataclass
class PlannedLink:
    index: int
    kind: str           # core | host
    instance: int
    subnet: str
    a: Endpoint
    b: Endpoint


@dataclass
class PopPlan:
    pop: object
    isis_net: str
    blackhole: str      # locator /48
    loopback: str       # ::1/128
    interfaces: list    # [Interface]


@dataclass
class HostPlan:
    host: object
    attach_node: str
    instance: int       # ordinal among hosts on the attach pop
    subnet: str
    address: str        # ::2/64
    gateway: str        # ::1
    iface: str          # eth1
    peer_iface: str     # pop side eth


@dataclass
class Plan:
    pops: dict          # pop id -> PopPlan
    hosts: dict         # host id -> HostPlan
    links: list         # [PlannedLink], core first then host


def build_plan(topo):
    d = topo.defaults
    hinst = _host_instances(topo)   # host id -> ordinal on its attach pop
    iface_by_key = {}               # (pop id, ('core', link idx) | ('host', host id)) -> eth name
    pops = {}

    for pop in topo.pops:
        ifaces = []
        n = 0
        # canonical order: core links by (peer index, instance), then hosts by instance
        core = [l for l in topo.links if pop.id in (l.a, l.b)]
        core.sort(key=lambda l: (topo.pop_by_id(_peer(pop.id, l)).index, l.instance))
        for link in core:
            n += 1
            name = f"eth{n}"
            _, a_addr, b_addr = addressing.link_addrs(d.link_prefix, link.index)
            peer = topo.pop_by_id(_peer(pop.id, link)).node_name
            addr = a_addr if pop.id == link.a else b_addr
            ifaces.append(Interface(name, "core", peer, addr, f"core link to {peer}"))
            iface_by_key[(pop.id, ("core", link.index))] = name

        attached = sorted((h for h in topo.hosts if h.attach == pop.id), key=lambda h: hinst[h.id])
        for host in attached:
            n += 1
            name = f"eth{n}"
            _, pop_addr, _, _ = addressing.host_addrs(d.host_prefix, addressing.host_subnet_index(pop.index, hinst[host.id]))
            ifaces.append(Interface(name, "host", host.node_name, pop_addr, f"host link to {host.node_name}"))
            iface_by_key[(pop.id, ("host", host.id))] = name

        blackhole, loopback = addressing.locator(d.locator_prefix, pop.index)
        pops[pop.id] = PopPlan(pop, addressing.isis_net(pop.index), blackhole, loopback, ifaces)

    links = []
    for link in sorted(topo.links, key=lambda l: l.index):
        subnet, a_addr, b_addr = addressing.link_addrs(d.link_prefix, link.index)
        a = topo.pop_by_id(link.a)
        b = topo.pop_by_id(link.b)
        links.append(PlannedLink(
            link.index, "core", link.instance, subnet,
            Endpoint(a.node_name, iface_by_key[(link.a, ("core", link.index))], a_addr),
            Endpoint(b.node_name, iface_by_key[(link.b, ("core", link.index))], b_addr),
        ))

    hosts = {}
    for host in topo.hosts:
        attach = topo.pop_by_id(host.attach)
        sidx = addressing.host_subnet_index(attach.index, hinst[host.id])
        subnet, pop_addr, host_addr, gw = addressing.host_addrs(d.host_prefix, sidx)
        pop_iface = iface_by_key[(host.attach, ("host", host.id))]
        hosts[host.id] = HostPlan(host, attach.node_name, hinst[host.id], subnet, host_addr, gw, "eth1", pop_iface)
        links.append(PlannedLink(
            sidx, "host", hinst[host.id], subnet,
            Endpoint(attach.node_name, pop_iface, pop_addr),
            Endpoint(host.node_name, "eth1", host_addr),
        ))

    return Plan(pops, hosts, links)


def _peer(pop_id, link):
    return link.b if pop_id == link.a else link.a


def _host_instances(topo):
    counts = {}
    instance = {}
    for h in topo.hosts:
        counts[h.attach] = counts.get(h.attach, 0) + 1
        instance[h.id] = counts[h.attach]
    return instance
