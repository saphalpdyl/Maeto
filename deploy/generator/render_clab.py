import yaml

from .constants import FRR_IMAGE, HOST_IMAGE


class _Flow(list):
    # renders inline, e.g. endpoints: [PopA:eth1, PopB:eth1]
    pass


yaml.SafeDumper.add_representer(
    _Flow,
    lambda d, data: d.represent_sequence("tag:yaml.org,2002:seq", data, flow_style=True),
)


def render_clab(topo, plan):
    nodes = {}
    for pop in topo.pops:
        nodes[pop.node_name] = {
            "kind": "linux",
            "image": FRR_IMAGE,
            "binds": [
                "conf/shared/frr_daemons:/etc/frr/daemons",
                f"conf/{pop.node_name}/frr.conf:/etc/frr/frr.conf",
            ],
            "exec": [
                "sysctl -w net.ipv6.conf.all.forwarding=1",
                "sysctl -w net.ipv6.conf.all.seg6_enabled=1",
            ],
            "labels": {"clab_label": pop.clab_label},
        }
    for host in topo.hosts:
        hp = plan.hosts[host.id]
        nodes[host.node_name] = {
            "kind": "linux",
            "image": HOST_IMAGE,
            "exec": [
                f"ip link set dev {hp.iface} up",
                f"ip -6 addr add {hp.address} dev {hp.iface}",
                f"ip -6 route add default via {hp.gateway}",
            ],
            "labels": {"clab_label": host.clab_label},
        }

    links = [
        {"endpoints": _Flow([f"{l.a.node}:{l.a.iface}", f"{l.b.node}:{l.b.iface}"])}
        for l in plan.links
    ]

    doc = {
        "name": topo.name,
        "topology": {
            # eth0 (management) disabled for every node, interfaces start at eth1
            "defaults": {"network-mode": "none"},
            "nodes": nodes,
            "links": links,
        },
    }
    return yaml.safe_dump(doc, sort_keys=False, default_flow_style=False, width=120)
