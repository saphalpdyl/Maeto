from .constants import INTERNET_MGMT_ADDRESS


def render_mgmt_routes(topo, plan):
    # the cpes reach nats through the internet router, but the management
    # bridge only knows its own /64, so replies are dropped on the way back.
    # run on the container host, where the bridge gateway lives.
    lines = [
        "#!/bin/sh",
        "# return routes from the management bridge to the cpe prefixes",
        "set -e",
        "",
    ]

    if plan.transit is not None:
        lines.append(
            f"ip -6 route replace {topo.defaults.edge_prefix} via {INTERNET_MGMT_ADDRESS}"
        )

    return "\n".join(lines) + "\n"
