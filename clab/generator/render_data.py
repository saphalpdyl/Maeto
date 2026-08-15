import json

from .constants import GENERATOR_VERSION


def render_data(topo, plan, digest):
    doc = {
        "name": topo.name,
        "topology_sha256": digest,
        "generator_version": GENERATOR_VERSION,
        "defaults": {
            "locator_prefix": topo.defaults.locator_prefix,
            "link_prefix": topo.defaults.link_prefix,
            "host_prefix": topo.defaults.host_prefix,
        },
        "pops": [_pop(topo.pop_by_id(p.id), plan.pops[p.id]) for p in topo.pops],
        "hosts": [_host(h, plan.hosts[h.id]) for h in topo.hosts],
        "links": [_link(l) for l in plan.links],
    }
    return json.dumps(doc, indent=2) + "\n"


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
        "data": pop.data,
    }


def _host(host, hp):
    return {
        "id": host.id,
        "name": host.node_name,
        "clab_label": host.clab_label,
        "instance": hp.instance,
        "attach": host.attach,
        "attach_node": hp.attach_node,
        "subnet": hp.subnet,
        "address": hp.address,
        "gateway": hp.gateway,
        "interface": hp.iface,
        "peer_interface": hp.peer_iface,
        "data": host.data,
    }


def _link(l):
    return {
        "index": l.index,
        "type": l.kind,
        "instance": l.instance,
        "subnet": l.subnet,
        "a": {"node": l.a.node, "interface": l.a.iface, "address": l.a.address},
        "b": {"node": l.b.node, "interface": l.b.iface, "address": l.b.address},
    }
