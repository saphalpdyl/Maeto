from .constants import FRR_VERSION, ISIS_INSTANCE, ROLE_PE


def render_frr(pop_plan):
    p = pop_plan
    lines = [
        f"frr version {FRR_VERSION}",
        "frr defaults traditional",
        f"hostname {p.pop.node_name}",
        "log stdout",
        "log file /var/log/frr/frr.log debugging",
        "service integrated-vtysh-config",
        "!",
        "interface lo",
        f" ipv6 address {p.loopback}",
        f" ipv6 router isis {ISIS_INSTANCE}",
        " isis passive",
        "!",
        "interface sr0",
        f" ipv6 address {p.mirror_loopback_prefix}",
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
            " isis bfd",
            " isis bfd profile maeto-core",
            "!",
        ]

    lines += [
        "segment-routing",
        " srv6",
        "  locators",
        "   locator CORE",
       f"    prefix {p.locator_prefix} block-len 32 node-len 16 func-bits 16", # bbbb:bbbb:nnnn:ffff::
        "   !",
        "   locator MIRROR",
       f"    prefix {p.mirror_prefix} block-len 32 node-len 16 func-bits 16", # bbbb:bbbb:nnnn:ffff::
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

    lines += [
        "bfd",
        " profile maeto-core",
        "  transmit-interval 10",
        "  receive-interval 10",
        "  detect-multiplier 4",
        " !",
        "!",
    ]

    # the cpe profile is for bfd inside customer tunnels, which only a pe has
    if p.pop.role != ROLE_PE:
        return "\n".join(lines)

    lines += [
        "bfd",
        " profile maeto-cpe",
        "  transmit-interval 100",
        "  receive-interval 100",
        "  detect-multiplier 3",
        " !",
        "!",
    ]

    return "\n".join(lines)


def render_frr_cpe(cpe):
    lines = [
        f"frr version {FRR_VERSION}",
        "frr defaults traditional",
        f"hostname {cpe.node_name}",
        "log stdout",
        "log file /var/log/frr/frr.log debugging",
        "service integrated-vtysh-config",
        "!",
        # tunnels ride the internet, so the timers are far looser than the
        # core's; portald adds a peer per tunnel at runtime
        "bfd",
        " profile maeto-cpe",
        "  transmit-interval 100",
        "  receive-interval 100",
        "  detect-multiplier 3",
        " !",
        "!",
    ]

    return "\n".join(lines)
