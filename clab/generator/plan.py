from dataclasses import dataclass

from . import addressing
from .constants import (
    CPE_IFACE,
    CPE_LAN_IFACE,
    HOST_IFACE,
    POP_TRANSIT_IFACE,
    TRANSIT_UPLINK_IFACE,
)


@dataclass
class Interface:
    name: str           # eth2
    role: str           # core
    peer: str           # peer node name
    address: str        # ::1/64
    description: str


@dataclass
class AccessPlan:
    # the tenant-facing side of a pop, owned by iproute2 and nftables rather
    # than frr: it stays out of isis so nothing it holds is advertised to the
    # core, and a forward-chain drop keeps anything arriving on it from being
    # forwarded anywhere at all
    iface: str
    address: str        # ::1/64 on the transit link
    aggregate: str      # /56 covering the uplink and every cpe behind the transit
    nexthop: str        # transit router's ::2


@dataclass
class Endpoint:
    kind: str
    id: str
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
    locator_prefix: str # the pop's /48, sids are carved from it
    loopback: str       # ::1/128
    interfaces: list    # [Interface], core links only -- what frr owns
    access: object      # AccessPlan, or None on a pop with no cpes


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
    lan_iface: str      # eth2, faces the tenant lan
    lan_address: str    # ::1/64 out of the site prefix


@dataclass
class HostPlan:
    # the tenant's own kit behind a cpe. it has no management interface, so the
    # only way in or out is the cpe and, past that, the tunnel
    cpe_id: str
    node_name: str      # HostA2
    clab_label: str
    iface: str          # eth1
    address: str        # digest-derived, inside the site prefix
    gateway: str        # the cpe's lan address
    subnet: str
    peer_iface: str     # cpe side eth2


@dataclass
class Plan:
    pops: dict          # pop id -> PopPlan
    transits: dict      # pop id -> TransitPlan, only for pops that have cpes
    cpes: dict          # cpe id -> CpePlan
    hosts: dict         # cpe id -> HostPlan, one lan host each
    links: list         # [PlannedLink], core first then transit then cpe then lan


def build_plan(topo):
    d = topo.defaults
    attached = _attached_cpes(topo)      # pop id -> [Cpe] in declaration order
    cinst = {c.id: n for cpes in attached.values() for n, c in enumerate(cpes, 1)}
    iface_by_key = {}                    # (pop id, ('core', link idx)) -> eth name
    pops = {}

    for pop in topo.pops:
        ifaces = []
        access = None
        if attached.get(pop.id):
            uplink = addressing.edge_subnet_index(pop.index, 0)
            _, pop_addr, _, _, transit_addr = addressing.edge_addrs(d.edge_prefix, uplink)
            # the cpes sit a hop further out, so the pop reaches them through one
            # aggregate handed to the transit router. it is never redistributed --
            # only this pop needs it, and advertising it would hand the whole
            # backbone a route back into tenant space
            access = AccessPlan(POP_TRANSIT_IFACE, pop_addr,
                                addressing.edge_aggregate(d.edge_prefix, pop.index), transit_addr)

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

        locator_prefix, loopback = addressing.locator(d.locator_prefix, pop.index)
        pops[pop.id] = PopPlan(pop, addressing.isis_net(pop.index), locator_prefix, loopback, ifaces, access)

    links = []
    for link in sorted(topo.links, key=lambda l: l.index):
        subnet, a_addr, b_addr = addressing.link_addrs(d.link_prefix, link.index)
        a = topo.pop_by_id(link.a)
        b = topo.pop_by_id(link.b)
        links.append(PlannedLink(
            link.index, "core", link.instance, subnet,
            Endpoint("pop", a.id, a.node_name, iface_by_key[(link.a, ("core", link.index))], a_addr),
            Endpoint("pop", b.id, b.node_name, iface_by_key[(link.b, ("core", link.index))], b_addr),
        ))

    transits = {}
    cpes = {}
    hosts = {}
    lan_links = []      # kept separate so every lan link sorts after every cpe link
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
            Endpoint("pop", pop.id, pop.node_name, POP_TRANSIT_IFACE, pop_addr),
            Endpoint("transit", pop.id, transit_node, TRANSIT_UPLINK_IFACE, transit_addr),
        ))

        n = 1
        for cpe in attached_here:
            n += 1
            iface = f"eth{n}"
            idx = addressing.edge_subnet_index(pop.index, cinst[cpe.id])
            subnet, transit_side, cpe_addr, gw, _ = addressing.edge_addrs(d.edge_prefix, idx)
            transit_ifaces.append(TransitIface(iface, "cpe", cpe.node_name, transit_side))

            lan_subnet, lan_gw_addr, host_addr, lan_gw = addressing.lan_addrs(cpe.prefix, cpe.id)
            cpes[cpe.id] = CpePlan(cpe, pop.node_name, transit_node, cinst[cpe.id],
                                   subnet, cpe_addr, gw, CPE_IFACE, iface,
                                   CPE_LAN_IFACE, lan_gw_addr)
            links.append(PlannedLink(
                idx, "cpe", cinst[cpe.id], subnet,
                Endpoint("transit", pop.id, transit_node, iface, transit_side),
                Endpoint("cpe", cpe.id, cpe.node_name, CPE_IFACE, cpe_addr),
            ))

            host_node = _host_node_name(cpe)
            hosts[cpe.id] = HostPlan(cpe.id, host_node, f"Host {cpe.id}", HOST_IFACE,
                                     host_addr, lan_gw, lan_subnet, CPE_LAN_IFACE)
            lan_links.append(PlannedLink(
                idx, "lan", cinst[cpe.id], lan_subnet,
                Endpoint("cpe", cpe.id, cpe.node_name, CPE_LAN_IFACE, lan_gw_addr),
                Endpoint("host", cpe.id, host_node, HOST_IFACE, host_addr),
            ))

        transits[pop.id] = TransitPlan(pop.id, transit_node, f"Transit {pop.id}",
                                       pop.node_name, pop_gw, transit_ifaces)

    return Plan(pops, transits, cpes, hosts, links + lan_links)


def _peer(pop_id, link):
    return link.b if pop_id == link.a else link.a


def _transit_node_name(pop):
    return f"Transit{pop.id}"


def _host_node_name(cpe):
    # CpeA2 -> HostA2, matching how cpe node names drop the leading marker
    return f"Host{cpe.id[1:]}"


def _attached_cpes(topo):
    by_pop = {}
    for c in topo.cpes:
        by_pop.setdefault(c.attach, []).append(c)
    return by_pop
