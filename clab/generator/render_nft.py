# ns/na are addressed to link-local and solicited-node multicast, never to the
# pop's edge address, so these cannot be scoped by daddr without breaking the link
ICMPV6_LINK_LOCAL = "nd-neighbor-solicit, nd-neighbor-advert"
# everything else is scoped to the edge address like ike and esp are
ICMPV6_SCOPED = "packet-too-big, echo-request"


def render_nft(access):
    # The access interface is host-terminated. IKE and ESP are addressed to the
    # pop itself, so they land in input; decapsulated tenant traffic re-enters
    # on an xfrm interface, never on this one. Nothing arriving here is ever
    # legitimately forwarded, in any direction, to anywhere -- which is a
    # forwarding property, and one forward-chain drop states it exactly. A vrf
    # states reachability instead, needs a hand-installed unreachable route to
    # stop leaking into the backbone, and still misses the cpe -> pop -> cpe
    # hairpin that this covers for free.
    i = access.iface
    addr = access.address.split("/", 1)[0]
    return "\n".join([
        "table inet maeto {",
        "  chain forward {",
        "    type filter hook forward priority filter; policy accept;",
        "",
        "    # counters on every drop: Ip6OutForwDatagrams counts *attempted*",
        "    # forwards, so without these the only way to tell a working rule from",
        "    # a broken one is to remove it and watch for a routing loop",
        f'    iifname "{i}" counter drop',
        "  }",
        "",
        "  chain input {",
        "    type filter hook input priority filter; policy accept;",
        "",
        "    # anti-spoofing: the source has to be routable back out the interface",
        "    # it arrived on, which a forged core or loopback address is not",
        f'    iifname "{i}" fib saddr . iif oif missing counter drop',
        "",
        "    # the pop's own edge address is the only thing a cpe may talk to, so",
        "    # the loopback, the srv6 locator and every core link stay unreachable",
        "    # from the access side. 4500 is accepted even without nat because",
        "    # strongswan floats there on its own detection and a miss would fail",
        "    # confusingly for no benefit.",
        f'    iifname "{i}" ip6 daddr {addr} udp dport {{ 500, 4500 }} accept',
        f'    iifname "{i}" ip6 daddr {addr} meta l4proto esp accept',
        "",
        "    # neighbour discovery is mandatory or the link goes silent. it stays",
        "    # unscoped because ns/na are not addressed to the edge address.",
        f'    iifname "{i}" icmpv6 type {{ {ICMPV6_LINK_LOCAL} }} accept',
        "",
        "    # scoped like ike/esp, so a cpe cannot reach the loopback, the locator",
        "    # or any core link. packet-too-big keeps pmtud working once tunnel",
        "    # headers eat into the mtu; echo is a lab convenience, safe to drop.",
        f'    iifname "{i}" ip6 daddr {addr} icmpv6 type {{ {ICMPV6_SCOPED} }} accept',
        "",
        f'    iifname "{i}" counter drop',
        "  }",
        "}",
        "",
    ])
