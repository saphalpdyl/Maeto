# Finds the core link a ping is actually crossing: the default-class route
# on the source's PE gives the segment list, and the first segment that is a
# P router's End.X onto another P router names the link. End.X numbering
# changes whenever a router restarts, so this is read live, never stored.

import ipaddress
import re

from typing import List, Optional

from lab import Host, Lab, Link
from shaping import Shaper

SEGS = re.compile(r"segs \d+ \[([^\]]*)\]")
OIF = re.compile(r"\boif (\S+)")
TABLE = re.compile(r"iif maeto-vrf-\S+ lookup (\d+)")


class PathError(Exception):
    pass


async def segments(shaper: Shaper, container: str, subnet: str) -> List[str]:
    rules = await shaper.run(container, ["ip", "-6", "rule"])
    for line in rules.splitlines():
        if "fwmark" in line:
            continue
        table = TABLE.search(line)
        if not table:
            continue

        routes = await shaper.run(container, ["ip", "-6", "route", "show", "table", table.group(1), "exact", subnet])
        for route in routes.splitlines():
            if "metric 2048" in route:
                continue
            segs = SEGS.search(route)
            if segs:
                return segs.group(1).split()

    return []


async def ping_link(lab: Lab, shaper: Shaper, source: Host, target: Host) -> Link:
    pe = lab.node(source.pe)
    if pe is None:
        raise PathError(f"{source.name} has no attach PE")

    segs = await segments(shaper, pe.container, target.subnet)
    if not segs:
        raise PathError(f"{pe.name} has no segment list towards {target.name}")

    for sid in segs:
        owner = owner_of(lab, sid)
        if owner is None or not owner.core:
            continue

        routes = await shaper.run(owner.container, ["ip", "-6", "route", "show"])
        for route in routes.splitlines():
            if not route.startswith(f"{sid} ") or "End.X" not in route:
                continue
            oif = OIF.search(route)
            link = lab.link_at(owner.id, oif.group(1)) if oif else None
            if link and lab.nodes[link.a.pop].core and lab.nodes[link.b.pop].core:
                return link

    raise PathError(f"{source.name} → {target.name} crosses no core link in {' '.join(segs)}")


def owner_of(lab: Lab, sid: str):
    addr = ipaddress.IPv6Address(sid)
    for node in lab.nodes.values():
        if node.locator and addr in node.locator:
            return node
    return None
