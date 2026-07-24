from .constants import FRR_VERSION, ISIS_INSTANCE


def render_frr(pop_plan):
    p = pop_plan
    lines = [
        f"frr version {FRR_VERSION}",
        "frr defaults traditional",
        f"hostname {p.pop.node_name}",
        "log stdout",
        "service integrated-vtysh-config",
        "!",
        "interface lo",
        f" ipv6 address {p.loopback}",
        f" ipv6 router isis {ISIS_INSTANCE}",
        " isis passive",
        "!",
    ]
    for i in p.interfaces:
        lines += [
            f"interface {i.name}",
            f" description {i.description}",
            f" ipv6 address {i.address}",
            f" ipv6 router isis {ISIS_INSTANCE}",
        ]
        # core links form adjacencies, host links only advertise the prefix
        lines.append(" isis network point-to-point" if i.role == "core" else " isis passive")
        lines.append("!")

    lines += [
        "segment-routing",
        " srv6",
        "  locators",
        "   locator CORE",
       f"    prefix {p.blackhole} block-len 32 node-len 16 func-bits 16",
        "   !",
        "  !",
        " !",
        "!",
    ]
    lines += [
        f"ipv6 route {p.blackhole} blackhole",
        "!",
        f"router isis {ISIS_INSTANCE}",
        f" net {p.isis_net}",
        " is-type level-1",
        " metric-style wide",
        " topology ipv6-unicast",
        " redistribute ipv6 static level-1",
        " segment-routing srv6",
        "  locator CORE",
        " !",
        "!",
    ]

    return "\n".join(lines)
