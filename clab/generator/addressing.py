import hashlib
import ipaddress

from .constants import (
    CPE_INSTANCE_BITS,
    LAN_HOST_BITS,
    EDGE_AGGREGATE_PREFIXLEN,
    ISIS_AREA,
    LINK_INSTANCE_BITS,
    LINK_POP_BITS,
)
from .errors import TopologyError


def _nth_subnet(prefix, new_prefix, n):
    # nth /new_prefix subnet inside prefix
    net = ipaddress.ip_network(prefix, strict=True)
    if n >= (1 << (new_prefix - net.prefixlen)):
        raise TopologyError(f"subnet index {n} does not fit within {prefix}")
    step = 1 << (net.max_prefixlen - new_prefix)
    addr = int(net.network_address) + n * step
    return ipaddress.ip_network((addr, new_prefix))


def link_subnet_index(lo_idx, hi_idx, instance):
    # stable index from the two endpoint pop indices + redundancy instance;
    # independent of the link's position in the links list
    return (lo_idx << (LINK_POP_BITS + LINK_INSTANCE_BITS)) | (hi_idx << LINK_INSTANCE_BITS) | (instance - 1)


def edge_subnet_index(attach_idx, instance):
    # stable index inside the attach pop's slice of the edge prefix; instance 0
    # is the pop <-> transit uplink and cpes behind that transit take 1..n
    return (attach_idx << CPE_INSTANCE_BITS) | instance


def _host(net, offset):
    return ipaddress.IPv6Address(int(net.network_address) + offset)


def locator(prefix, idx):
    # returns (locator /48, loopback ::1/128) for the pop
    net = _nth_subnet(prefix, 48, idx)
    return str(net), f"{_host(net, 1)}/128"


def link_addrs(prefix, idx):
    # returns (subnet /64, a ::1/64, b ::2/64)
    net = _nth_subnet(prefix, 64, idx)
    return str(net), f"{_host(net, 1)}/64", f"{_host(net, 2)}/64"


def edge_addrs(prefix, idx):
    # returns (subnet /64, near ::1/64, far ::2/64, near ::1, far ::2)
    # near is the upstream side of the link (the pop on a pop <-> transit
    # uplink, the transit router on a transit <-> cpe link), far is the
    # downstream side; the bare forms are what a default route or a static
    # nexthop points at
    net = _nth_subnet(prefix, 64, idx)
    near, far = _host(net, 1), _host(net, 2)
    return str(net), f"{near}/64", f"{far}/64", str(near), str(far)


def edge_aggregate(prefix, attach_idx):
    # the pop's whole slice of the edge prefix: its transit uplink plus every
    # cpe subnet behind that transit, so the pop needs one static route not n
    return str(_nth_subnet(prefix, EDGE_AGGREGATE_PREFIXLEN, attach_idx))


def lan_addrs(prefix, seed):
    # returns (subnet, cpe ::1/len, host <derived>/len, cpe ::1)
    # the host sits at a digest-derived offset so it looks like real kit rather
    # than ::2, while staying identical run to run
    net = ipaddress.ip_network(prefix, strict=True)
    if net.max_prefixlen - net.prefixlen <= LAN_HOST_BITS:
        raise TopologyError(f"site prefix {prefix} is too small for a lan host")

    digest = hashlib.sha256(seed.encode()).digest()
    span = (1 << LAN_HOST_BITS) - 2
    # ::0 is subnet-router anycast and ::1 is the cpe itself
    offset = 2 + (int.from_bytes(digest[: LAN_HOST_BITS // 8], "big") % span)

    gw = _host(net, 1)
    return (str(net), f"{gw}/{net.prefixlen}",
            f"{_host(net, offset)}/{net.prefixlen}", str(gw))


def isis_net(idx):
    sid = format(idx, "012x")
    return f"{ISIS_AREA}.{sid[0:4]}.{sid[4:8]}.{sid[8:12]}.00"
