from .constants import FRR_VERSION, ISIS_INSTANCE


def render_frr(pop_plan):
    p = pop_plan
    lines = [
        f"frr version {FRR_VERSION}",
        "frr defaults traditional",
        f"hostname {p.pop.node_name}",
        "log stdout",
        "log file /tmp/frr.log debugging",
        "service integrated-vtysh-config",
        "!",
        "interface lo",
        f" ipv6 address {p.loopback}",
        f" ipv6 router isis {ISIS_INSTANCE}",
        " isis passive",
        "!",
    ]
    # only core links reach frr. the tenant-facing interface is deliberately
    # absent: it lives in the access vrf, configured by iproute2, so there is no
    # stanza here that could advertise tenant space into the backbone
    for i in p.interfaces:
        lines += [
            f"interface {i.name}",
            f" description {i.description}",
            f" ipv6 address {i.address}",
            f" ipv6 router isis {ISIS_INSTANCE}",
            " isis network point-to-point",
            "!",
        ]

    lines += [
        "segment-routing",
        " srv6",
        "  locators",
        "   locator CORE",
       f"    prefix {p.blackhole} block-len 32 node-len 16 func-bits 16",
       f"    behavior usid",
        "   !",
        "  !",
        " !",
        "!",
    ]
    lines += [
        f"router isis {ISIS_INSTANCE}",
        f" net {p.isis_net}",
        " is-type level-1",
        " metric-style wide",
        " topology ipv6-unicast",
        # no redistribute: the backbone carries core prefixes and locators only,
        # never anything learned from or about a tenant
        " segment-routing srv6",
        "  locator CORE",
        " !",
        "!",
    ]

    return "\n".join(lines)
