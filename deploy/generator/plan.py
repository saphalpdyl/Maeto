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
    iface_by_key = {}   # (pop id, ('core'|'host', idx)) -> eth name
    pops = {}

    for pop in topo.pops:
        ifaces = []
        n = 0
        for link in topo.links:
            if pop.id not in (link.a, link.b):
                continue
            n += 1
            name = f"eth{n}"
            _, a_addr, b_addr = addressing.link_addrs(d.link_prefix, link.index)
            peer_id = link.b if pop.id == link.a else link.a
            peer = topo.pop_by_id(peer_id).node_name
            addr = a_addr if pop.id == link.a else b_addr
            ifaces.append(Interface(name, "core", peer, addr, f"core link to {peer}"))
            iface_by_key[(pop.id, ("core", link.index))] = name

        for host in topo.hosts:
            if host.attach != pop.id:
                continue
            n += 1
            name = f"eth{n}"
            _, pop_addr, _, _ = addressing.host_addrs(d.host_prefix, host.index)
            ifaces.append(Interface(name, "host", host.node_name, pop_addr, f"host link to {host.node_name}"))
            iface_by_key[(pop.id, ("host", host.index))] = name

        blackhole, loopback = addressing.locator(d.locator_prefix, pop.index)
        pops[pop.id] = PopPlan(pop, addressing.isis_net(pop.index), blackhole, loopback, ifaces)

    links = []
    for link in topo.links:
        subnet, a_addr, b_addr = addressing.link_addrs(d.link_prefix, link.index)
        a = topo.pop_by_id(link.a)
        b = topo.pop_by_id(link.b)
        links.append(PlannedLink(
            link.index, "core", subnet,
            Endpoint(a.node_name, iface_by_key[(link.a, ("core", link.index))], a_addr),
            Endpoint(b.node_name, iface_by_key[(link.b, ("core", link.index))], b_addr),
        ))

    hosts = {}
    for host in topo.hosts:
        subnet, pop_addr, host_addr, gw = addressing.host_addrs(d.host_prefix, host.index)
        pop_node = topo.pop_by_id(host.attach).node_name
        pop_iface = iface_by_key[(host.attach, ("host", host.index))]
        hosts[host.id] = HostPlan(host, pop_node, subnet, host_addr, gw, "eth1", pop_iface)
        links.append(PlannedLink(
            host.index, "host", subnet,
            Endpoint(pop_node, pop_iface, pop_addr),
            Endpoint(host.node_name, "eth1", host_addr),
        ))

    return Plan(pops, hosts, links)
