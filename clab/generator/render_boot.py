from .constants import BOOT_WAIT_STEPS
from .render_clab import pop_boot_commands


def render_boot(plan, pop_id):
    # Runs once per pop container. On a normal deploy every interface already
    # exists and the wait falls straight through; after pop-crash/pop-boot the
    # container comes up with only eth0 and its parked links are moved in a
    # moment later, so nothing may start before they are all back.
    pp = plan.pops[pop_id]
    ifaces = [i.name for i in pp.interfaces]
    if pp.access is not None:
        ifaces.insert(0, pp.access.iface)

    lines = [
        "#!/bin/sh",
        "# rendered by the generator; see render_boot.py",
        "",
        f"for i in $(seq {BOOT_WAIT_STEPS}); do",
        "  missing=0",
        f"  for iface in {' '.join(ifaces)}; do",
        "    [ -e /sys/class/net/$iface ] || missing=1",
        "  done",
        '  [ "$missing" -eq 0 ] && break',
        "  sleep 0.2",
        "done",
        "",
    ]
    lines += pop_boot_commands(plan, pop_id)
    return "\n".join(lines) + "\n"
