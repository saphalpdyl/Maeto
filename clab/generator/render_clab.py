import yaml

from .constants import CPE_IMAGE, FRR_IMAGE, TRANSIT_IMAGE


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
                "ip link add sr0 type dummy",
                "ip link set sr0 up"
            ],
            "labels": {"clab_label": pop.clab_label},
        }

    for transit in plan.transits.values():
        cmds = [
            "sysctl -w net.ipv4.ip_forward=1",
            "sysctl -w net.ipv6.conf.all.forwarding=1",
        ]
        for i in transit.interfaces:
            cmds.append(f"ip link set dev {i.name} up")
            cmds.append(f"ip -6 addr replace {i.address} dev {i.name}")
        # the transit router keeps its management eth0, so docker has already
        # installed a v6 default there; replace it or the lab default never takes
        # effect. the management subnet stays reachable over eth0 as a connected
        # route.
        cmds.append(f"ip -6 route replace default via {transit.gateway}")
        nodes[transit.node_name] = {
            "kind": "linux",
            "image": TRANSIT_IMAGE,
            "exec": cmds,
            "labels": {"clab_label": transit.clab_label},
        }

    for cpe in topo.cpes:
        cp = plan.cpes[cpe.id]
        nodes[cpe.node_name] = {
            "kind": "linux",
            "image": CPE_IMAGE,
            # no management eth0: a cpe is a customer endpoint and must reach
            # everything through its transit router, never out of band via docker
            "network-mode": "none",
            "exec": [
                f"ip link set dev {cp.iface} up",
                f"ip -6 addr replace {cp.address} dev {cp.iface}",
                f"ip -6 route replace default via {cp.gateway}",
            ],
            "labels": {"clab_label": cpe.clab_label},
        }

    links = [
        {"endpoints": _Flow([f"{l.a.node}:{l.a.iface}", f"{l.b.node}:{l.b.iface}"])}
        for l in plan.links
    ]

    nodes["nats"] = {
        "kind": "linux",
        "image": "nats:2.12.7",
        "cmd": "--http_port 8222 -js -sd /data",
        "labels": {"clab_label": "NATS JetStream Server"},
        "group": "control",
        "mgmt-ipv6": "3fff:172:20:20::800:11"
    }

    doc = {
        "name": topo.name,
        "topology": {
            "nodes": nodes,
            "links": links,
        },
    }
    return yaml.safe_dump(doc, sort_keys=False, default_flow_style=False, width=120)
