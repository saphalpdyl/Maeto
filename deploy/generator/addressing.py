import ipaddress

from .constants import ISIS_AREA


def _nth_subnet(prefix, new_prefix, n):
    # nth /new_prefix subnet inside prefix (n is 1-based here)
    net = ipaddress.ip_network(prefix, strict=True)
    step = 1 << (net.max_prefixlen - new_prefix)
    addr = int(net.network_address) + n * step
    return ipaddress.ip_network((addr, new_prefix))


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
