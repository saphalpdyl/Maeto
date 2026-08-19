import json

from .constants import GENERATOR_VERSION


def render_node(pop, pp):
    return json.dumps(_pop(pop, pp), indent=2) + "\n"


def render_data(topo, plan, digest):
    doc = {
        "name": topo.name,
        "topology_sha256": digest,
        "generator_version": GENERATOR_VERSION,
        "defaults": {
            "locator_prefix": topo.defaults.locator_prefix,
            "link_prefix": topo.defaults.link_prefix,
            "edge_prefix": topo.defaults.edge_prefix,
        },
        "pops": [_pop(topo.pop_by_id(p.id), plan.pops[p.id]) for p in topo.pops],
        "transits": [_transit(plan.transits[p.id]) for p in topo.pops if p.id in plan.transits],
        "cpes": [_cpe(c, plan.cpes[c.id]) for c in topo.cpes],
        "hosts": [_host(plan.hosts[c.id]) for c in topo.cpes],
        "links": [_link(l) for l in plan.links],
    }
    return json.dumps(doc, indent=2) + "\n"


def _host(h):
    return {
        "cpe": h.cpe_id,
        "name": h.node_name,
        "clab_label": h.clab_label,
        "interface": h.iface,
        "subnet": h.subnet,
        "address": h.address,
        "gateway": h.gateway,
    }


def _pop(pop, pp):
    return {
        "id": pop.id,
        "name": pop.node_name,
        "clab_label": pop.clab_label,
        "index": pop.index,
        "isis_net": pp.isis_net,
        "locator": pp.blackhole,
        "loopback": pp.loopback,
        "interfaces": [
            {
                "name": i.name,
                "role": i.role,
                "peer": i.peer,
                "address": i.address,
            }
            for i in pp.interfaces
        ],
        "access": _access(pp.access),
        "data": pop.data,
    }


def _access(a):
    if a is None:
        return None
    return {
        "interface": a.iface,
        "address": a.address,
        "aggregate": a.aggregate,
        "nexthop": a.nexthop,
    }


def _transit(tp):
    return {
        "id": tp.id,
        "name": tp.node_name,
        "clab_label": tp.clab_label,
        "pop": tp.id,
        "pop_node": tp.pop_node,
        "gateway": tp.gateway,
        "interfaces": [
            {
                "name": i.name,
                "role": i.role,
                "peer": i.peer,
                "address": i.address,
            }
            for i in tp.interfaces
        ],
    }


def _cpe(cpe, cp):
    return {
        "id": cpe.id,
        "name": cpe.node_name,
        "clab_label": cpe.clab_label,
        "instance": cp.instance,
        "attach": cpe.attach,
        "attach_node": cp.attach_node,
        "transit_node": cp.transit_node,
        "subnet": cp.subnet,
        "address": cp.address,
        "gateway": cp.gateway,
        "interface": cp.iface,
        "peer_interface": cp.peer_iface,
        "data": cpe.data,
    }


def _link(l):
    return {
        "index": l.index,
        "type": l.kind,
        "instance": l.instance,
        "subnet": l.subnet,
        "a": {"kind": l.a.kind, "id": l.a.id, "node": l.a.node, "interface": l.a.iface, "address": l.a.address},
        "b": {"kind": l.b.kind, "id": l.b.id, "node": l.b.node, "interface": l.b.iface, "address": l.b.address},
    }
