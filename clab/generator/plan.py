from dataclasses import dataclass

from . import addressing
from .constants import CPE_IFACE, POP_TRANSIT_IFACE, TRANSIT_UPLINK_IFACE


@dataclass
class Interface:
    name: str           # eth1
    role: str           # core | transit
    peer: str           # peer node name
    address: str        # ::1/64
    description: str


@dataclass
class StaticRoute:
    prefix: str
    nexthop: str
    iface: str


@dataclass
class Endpoint:
    node: str
    iface: str
    address: str


@dataclass
class PlannedLink:
    index: int
    kind: str           # core | transit | cpe
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
    statics: list       # [StaticRoute] covering the cpes behind the transit


@dataclass
class TransitIface:
    name: str
    role: str           # uplink | cpe
    peer: str
    address: str


@dataclass
class TransitPlan:
    id: str             # id of the pop this transit router fronts
    node_name: str      # TransitA
    clab_label: str
    pop_node: str
    gateway: str        # pop's eth1 address, the transit router's default route
    interfaces: list    # [TransitIface], uplink first then one per cpe


@dataclass
class CpePlan:
    cpe: object
    attach_node: str    # pop node the cpe is homed to
    transit_node: str   # transit router it physically plugs into
    instance: int       # ordinal among cpes on the attach pop
    subnet: str
    address: str        # ::2/64
    gateway: str        # transit router address on this link
    iface: str          # eth1
    peer_iface: str     # transit side eth


@dataclass
class Plan:
    pops: dict          # pop id -> PopPlan
    transits: dict      # pop id -> TransitPlan, only for pops that have cpes
    cpes: dict          # cpe id -> CpePlan
    links: list         # [PlannedLink], core first then transit then cpe


def build_plan(topo):
    d = topo.defaults
    attached = _attached_cpes(topo)      # pop id -> [Cpe] in declaration order
    cinst = {c.id: n for cpes in attached.values() for n, c in enumerate(cpes, 1)}
    iface_by_key = {}                    # (pop id, ('core', link idx)) -> eth name
    pops = {}

    for pop in topo.pops:
        ifaces = []
        statics = []
        if attached.get(pop.id):
            transit_node = _transit_node_name(pop)
            uplink = addressing.edge_subnet_index(pop.index, 0)
            _, pop_addr, _, _, transit_addr = addressing.edge_addrs(d.edge_prefix, uplink)
            ifaces.append(Interface(POP_TRANSIT_IFACE, "transit", transit_node, pop_addr,
                                    f"transit link to {transit_node}"))
            # the cpes sit a hop further out, so the pop reaches them through one
            # aggregate handed to the transit router and redistributed into isis.
            # the nexthop sits inside that aggregate, so pin the outgoing
            # interface rather than leaning on recursive resolution
            statics.append(StaticRoute(addressing.edge_aggregate(d.edge_prefix, pop.index),
                                       transit_addr, POP_TRANSIT_IFACE))

        # eth1 belongs to the transit link on every pop, cpes or not, so core
        # links always start at eth2 and never shift when cpes are added
        n = 1
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

        blackhole, loopback = addressing.locator(d.locator_prefix, pop.index)
        pops[pop.id] = PopPlan(pop, addressing.isis_net(pop.index), blackhole, loopback, ifaces, statics)

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

    transits = {}
    cpes = {}
    for pop in topo.pops:
        # every cpe on a pop shares that pop's transit router; a second cpe
        # attaching to the same pop hangs off the transit router, it does not
        # take another pop interface
        attached_here = attached.get(pop.id)
        if not attached_here:
            continue

        transit_node = _transit_node_name(pop)
        uplink = addressing.edge_subnet_index(pop.index, 0)
        subnet, pop_addr, transit_addr, pop_gw, _ = addressing.edge_addrs(d.edge_prefix, uplink)
        transit_ifaces = [TransitIface(TRANSIT_UPLINK_IFACE, "uplink", pop.node_name, transit_addr)]
        links.append(PlannedLink(
            uplink, "transit", 0, subnet,
            Endpoint(pop.node_name, POP_TRANSIT_IFACE, pop_addr),
            Endpoint(transit_node, TRANSIT_UPLINK_IFACE, transit_addr),
        ))

        n = 1
        for cpe in attached_here:
            n += 1
            iface = f"eth{n}"
            idx = addressing.edge_subnet_index(pop.index, cinst[cpe.id])
            subnet, transit_side, cpe_addr, gw, _ = addressing.edge_addrs(d.edge_prefix, idx)
            transit_ifaces.append(TransitIface(iface, "cpe", cpe.node_name, transit_side))
            cpes[cpe.id] = CpePlan(cpe, pop.node_name, transit_node, cinst[cpe.id],
                                   subnet, cpe_addr, gw, CPE_IFACE, iface)
            links.append(PlannedLink(
                idx, "cpe", cinst[cpe.id], subnet,
                Endpoint(transit_node, iface, transit_side),
                Endpoint(cpe.node_name, CPE_IFACE, cpe_addr),
            ))

        transits[pop.id] = TransitPlan(pop.id, transit_node, f"Transit {pop.id}",
                                       pop.node_name, pop_gw, transit_ifaces)

    return Plan(pops, transits, cpes, links)


def _peer(pop_id, link):
    return link.b if pop_id == link.a else link.a


def _transit_node_name(pop):
    return f"Transit{pop.id}"


def _attached_cpes(topo):
    by_pop = {}
    for c in topo.cpes:
        by_pop.setdefault(c.attach, []).append(c)
    return by_pop
