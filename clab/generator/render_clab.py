import os
import yaml

from .render_mgmt_routes import transit_mgmt_address
from .constants import (
    CA_CERT_BIND,
    CA_CERT_CONTAINER_PATH,
    CERT_CONTAINER_PATH,
    CPE_IMAGE,
    HOST_IMAGE,
    KEY_CONTAINER_PATH,
    NFT_CONTAINER_PATH,
    NFT_FILENAME,
    NATS_CLIENT_PORT,
    NATS_MGMT_ADDRESS,
    NATS_NODE_NAME,
    NODE_CONTAINER_PATH,
    NODE_FILENAME,
    DLV_LISTEN,
    OTEL_ENDPOINT,
    OTEL_SINK,
    POP_IMAGE,
    TRANSIT_IMAGE,
)


class _Flow(list):
    # renders inline, e.g. endpoints: [PopA:eth1, PopB:eth1]
    pass


yaml.SafeDumper.add_representer(
    _Flow,
    lambda d, data: d.represent_sequence("tag:yaml.org,2002:seq", data, flow_style=True),
)


def _access_setup(a):
    if a is None:
        return []
    # filter first, interface second. clab logs a failed exec and carries on, so
    # the reverse order would leave a working access interface with no isolation
    # at all -- open toward the backbone, which is the failure direction that
    # actually hurts. chained this way, a ruleset that will not load means eth1
    # never comes up and nothing moves. iifname matches by string, so the rules
    # load happily before the interface is configured.
    steps = " && ".join([
        f"nft -f {NFT_CONTAINER_PATH}",
        f"ip link set dev {a.iface} up",
        f"ip -6 addr replace {a.address} dev {a.iface}",
        f"ip -6 route replace {a.aggregate} via {a.nexthop} dev {a.iface}",
    ])
    return [f"sh -c '{steps}'"]


def _nats_url(topo):
    return f"nats://clab-{topo.name}-{NATS_NODE_NAME}:{NATS_CLIENT_PORT}"


# a cpe has no management interface and so no resolver: it must be given the
# address rather than the name
def _nats_url_literal():
    return f"nats://[{NATS_MGMT_ADDRESS}]:{NATS_CLIENT_PORT}"


def _debug_env():
    # MAETO_DEBUG=1 at generate time wires every node's delve listener
    if os.environ.get("MAETO_DEBUG") != "1":
        return {}
    return {"MAETO_DLV_LISTEN": DLV_LISTEN}


def _agent_env(nats_url):
    return {
        "NATS_CONNECT_URL": nats_url,
        "MAETO_NODE_FILE": NODE_CONTAINER_PATH,
        "OTEL_SINK": OTEL_SINK,
        "OTEL_ENDPOINT": OTEL_ENDPOINT,
        **_debug_env(),
    }


def _portal_env(cpe):
    return {
        "NATS_CONNECT_URL": _nats_url_literal(),
        "PORTAL_ID": cpe.portal_id,
        "OTEL_SINK": OTEL_SINK,
        "OTEL_ENDPOINT": OTEL_ENDPOINT,
        **_debug_env(),
    }


def render_clab(topo, plan):
    nats_url = _nats_url(topo)
    nodes = {}
    for pop in topo.pops:
        cmds = [
            "sysctl -w net.ipv6.conf.all.forwarding=1",
            "sysctl -w net.ipv6.conf.all.seg6_enabled=1",
            "ip link add sr0 type dummy",
            "ip link set sr0 up",
            "sh -c '/usr/lib/frr/docker-start &'"
        ]
        access = plan.pops[pop.id].access
        cmds += _access_setup(access)
        binds = [
            "conf/shared/frr_daemons:/etc/frr/daemons",
            f"conf/{pop.node_name}/frr.conf:/etc/frr/frr.conf",
            f"conf/{pop.node_name}/{NODE_FILENAME}:{NODE_CONTAINER_PATH}",
            f"{CA_CERT_BIND}:{CA_CERT_CONTAINER_PATH}",
            f"conf/{pop.node_name}/cert.pem:{CERT_CONTAINER_PATH}",
            f"conf/{pop.node_name}/key.pem:{KEY_CONTAINER_PATH}",
        ]
        if access is not None:
            binds.append(f"conf/{pop.node_name}/{NFT_FILENAME}:{NFT_CONTAINER_PATH}")
        nodes[pop.node_name] = {
            "kind": "linux",
            "image": POP_IMAGE,
            "binds": binds,
            "env": _agent_env(nats_url),
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
            "mgmt-ipv6": transit_mgmt_address(topo.pop_by_id(transit.id).index),
            "exec": cmds,
            "labels": {"clab_label": transit.clab_label},
        }

    for cpe in topo.cpes:
        cp = plan.cpes[cpe.id]
        nodes[cpe.node_name] = {
            "kind": "linux",
            "image": CPE_IMAGE,
            # no management eth0: a cpe is a tenant endpoint and must reach
            # everything through its transit router, never out of band via docker
            "network-mode": "none",
            "binds": [
                f"{CA_CERT_BIND}:{CA_CERT_CONTAINER_PATH}",
                f"conf/{cpe.node_name}/cert.pem:{CERT_CONTAINER_PATH}",
                f"conf/{cpe.node_name}/key.pem:{KEY_CONTAINER_PATH}",
            ],
            "env": _portal_env(cpe),
            "exec": [
                # without forwarding the cpe answers for itself but drops
                # anything its lan host sends toward the tunnel
                "sysctl -w net.ipv6.conf.all.forwarding=1",
                f"ip link set dev {cp.iface} up",
                f"ip -6 addr replace {cp.address} dev {cp.iface}",
                f"ip -6 route replace default via {cp.gateway}",
                f"ip link set dev {cp.lan_iface} up",
                f"ip -6 addr replace {cp.lan_address} dev {cp.lan_iface}",
            ],
            "labels": {"clab_label": cpe.clab_label},
        }

    for host in plan.hosts.values():
        nodes[host.node_name] = {
            "kind": "linux",
            "image": HOST_IMAGE,
            # like the cpe in front of it, no management interface: tenant
            # traffic has exactly one way out
            "network-mode": "none",
            "exec": [
                f"ip link set dev {host.iface} up",
                f"ip -6 addr replace {host.address} dev {host.iface}",
                f"ip -6 route replace default via {host.gateway} dev {host.iface}",
            ],
            "labels": {"clab_label": host.clab_label},
        }

    links = [
        {"endpoints": _Flow([f"{l.a.node}:{l.a.iface}", f"{l.b.node}:{l.b.iface}"])}
        for l in plan.links
    ]

    nodes[NATS_NODE_NAME] = {
        "kind": "linux",
        "image": "nats:2.12.7",
        "cmd": "--http_port 8222 -js -sd /data",
        "labels": {"clab_label": "NATS JetStream Server"},
        "group": "control",
        "mgmt-ipv6": NATS_MGMT_ADDRESS
    }

    doc = {
        "name": topo.name,
        "topology": {
            "nodes": nodes,
            "links": links,
        },
    }
    return yaml.safe_dump(doc, sort_keys=False, default_flow_style=False, width=120)
