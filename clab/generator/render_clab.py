import yaml

from .constants import CPE_IMAGE, FRR_IMAGE, TRANSIT_IMAGE, VRF_UNREACHABLE_METRIC


class _Flow(list):
    # renders inline, e.g. endpoints: [PopA:eth1, PopB:eth1]
    pass


yaml.SafeDumper.add_representer(
    _Flow,
    lambda d, data: d.represent_sequence("tag:yaml.org,2002:seq", data, flow_style=True),
)


def _access_vrf(a):
    # the customer-facing interface never lives in the global table, so a packet
    # from a cpe is resolved against a table holding only the transit link and the
    # aggregate behind it -- there is no core route to forward it onto. enslaving
    # flushes the interface's v6 addresses, so address it only after the move.
    if a is None:
        return []
    # chained into one command on purpose. clab logs a failed exec and carries on,
    # so as separate entries a missing vrf module would still let the addr land --
    # putting the customer interface in the global table with no route back, which
    # is both open and broken. chained, a failure leaves eth1 unconfigured: closed.
    steps = " && ".join([
        f"ip link add {a.vrf} type vrf table {a.table}",
        f"ip link set dev {a.vrf} up",
        f"ip link set dev {a.iface} master {a.vrf}",
        f"ip link set dev {a.iface} up",
        f"ip -6 addr replace {a.address} dev {a.iface}",
        f"ip -6 route replace {a.aggregate} via {a.nexthop} dev {a.iface} vrf {a.vrf}",
        # without this the vrf leaks: a miss falls through to the main table and
        # customer traffic is forwarded straight into the backbone
        f"ip -6 route replace unreachable default vrf {a.vrf} metric {VRF_UNREACHABLE_METRIC}",
    ])
    return [f"sh -c '{steps}'"]


def render_clab(topo, plan):
    nodes = {}
    for pop in topo.pops:
        cmds = [
            "sysctl -w net.ipv6.conf.all.forwarding=1",
            "sysctl -w net.ipv6.conf.all.seg6_enabled=1",
            "ip link add sr0 type dummy",
            "ip link set sr0 up",
        ]
        cmds += _access_vrf(plan.pops[pop.id].access)
        nodes[pop.node_name] = {
            "kind": "linux",
            "image": FRR_IMAGE,
            "binds": [
                "conf/shared/frr_daemons:/etc/frr/daemons",
                f"conf/{pop.node_name}/frr.conf:/etc/frr/frr.conf",
            ],
            "exec": cmds,
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
