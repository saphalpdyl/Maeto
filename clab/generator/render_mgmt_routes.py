from .constants import TRANSIT_MGMT_BASE


def transit_mgmt_address(index):
    return f"{TRANSIT_MGMT_BASE}{index:02x}"


def render_mgmt_routes(topo, plan):
    # the cpes reach nats through their transit router, but the management
    # bridge only knows its own /64, so replies are dropped on the way back.
    # run on the container host, where the bridge gateway lives.
    lines = [
        "#!/bin/sh",
        "# return routes from the management bridge to the cpe prefixes",
        "set -e",
        "",
    ]

    for t in plan.transits.values():
        access = plan.pops[t.id].access
        if access is None:
            continue

        index = topo.pop_by_id(t.id).index
        lines.append(
            f"ip -6 route replace {access.aggregate} via {transit_mgmt_address(index)}"
        )

    return "\n".join(lines) + "\n"
