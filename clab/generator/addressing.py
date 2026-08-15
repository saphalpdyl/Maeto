import ipaddress

from .constants import HOST_INSTANCE_BITS, ISIS_AREA, LINK_INSTANCE_BITS, LINK_POP_BITS
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


def host_subnet_index(attach_idx, instance):
    # stable index from the attach pop index + host instance on that pop
    return (attach_idx << HOST_INSTANCE_BITS) | (instance - 1)


def _host(net, offset):
    return ipaddress.IPv6Address(int(net.network_address) + offset)


def locator(prefix, idx):
    # returns (blackhole /48, loopback ::1/128) for the pop
    net = _nth_subnet(prefix, 48, idx)
    return str(net), f"{_host(net, 1)}/128"


def link_addrs(prefix, idx):
    # returns (subnet /64, a ::1/64, b ::2/64)
    net = _nth_subnet(prefix, 64, idx)
    return str(net), f"{_host(net, 1)}/64", f"{_host(net, 2)}/64"


def host_addrs(prefix, idx):
    # returns (subnet /64, pop ::1/64, host ::2/64, gateway ::1)
    net = _nth_subnet(prefix, 64, idx)
    gw = _host(net, 1)
    return str(net), f"{gw}/64", f"{_host(net, 2)}/64", str(gw)


def isis_net(idx):
    sid = format(idx, "012x")
    return f"{ISIS_AREA}.{sid[0:4]}.{sid[4:8]}.{sid[8:12]}.00"
