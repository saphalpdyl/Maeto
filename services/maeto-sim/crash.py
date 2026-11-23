# Crashes a whole node the way scripts/clab/pop-crash.sh does: its interfaces
# move into a parking container's netns, so the peers' veth ends survive, then
# the node is killed. Booting reverses it, like pop-boot.sh. Needs the sim to
# run privileged in the host pid namespace.

import asyncio
import logging
import subprocess
import time

from dataclasses import dataclass
from typing import Dict, List

import docker

from lab import Node

log = logging.getLogger("sim.crash")

PARK_IMAGE = "busybox:stable"
MODES = ("loud", "silent")


class CrashError(Exception):
    pass


@dataclass
class Crash:
    node: str
    mode: str
    source: str
    expires_at: float

    def view(self) -> dict:
        return {
            "node": self.node,
            "mode": self.mode,
            "source": self.source,
            "expires_at": self.expires_at,
            "remaining_s": max(0, round(self.expires_at - time.time())),
        }


def mode(spec: dict) -> str:
    value = spec.get("mode", "loud")
    if value not in MODES:
        raise CrashError(f"mode must be one of {', '.join(MODES)}")
    return value


class Crasher:
    def __init__(self, client: docker.DockerClient):
        self.client = client

    def _sh(self, command: List[str]):
        result = subprocess.run(command, capture_output=True, text=True)
        if result.returncode != 0:
            raise CrashError(f"{' '.join(command)}: {result.stderr.strip()}")

    def _pid(self, container: str) -> int:
        try:
            ctr = self.client.containers.get(container)
        except docker.errors.NotFound:
            raise CrashError(f"{container} does not exist")
        ctr.reload()
        pid = ctr.attrs["State"]["Pid"]
        if not pid:
            raise CrashError(f"{container} is not running")
        return pid

    def _park_name(self, node: Node) -> str:
        return f"maeto-park-{node.name}"

    def _crash(self, node: Node, how: str):
        park_name = self._park_name(node)
        try:
            self.client.containers.get(park_name)
            raise CrashError(f"{node.name} is already crashed")
        except docker.errors.NotFound:
            pass

        pid = self._pid(node.container)
        park = self.client.containers.run(
            PARK_IMAGE, ["sleep", "infinity"], name=park_name, network_mode="none", detach=True,
        )
        park_pid = self._pid(park.name)

        for iface in node.ifaces:
            self._sh(["nsenter", "-t", str(pid), "-n", "ip", "link", "set", iface, "netns", str(park_pid)])
            if how == "silent":
                self._sh(["nsenter", "-t", str(park_pid), "-n", "ip", "link", "set", iface, "up"])

        self.client.containers.get(node.container).kill()

    def _boot(self, node: Node):
        try:
            park = self.client.containers.get(self._park_name(node))
        except docker.errors.NotFound:
            raise CrashError(f"{node.name} is not crashed")

        ctr = self.client.containers.get(node.container)
        ctr.start()

        ctr.exec_run(["sh", "-c", "rm -f /var/run/charon.pid /var/run/charon.vici /var/run/frr/*.pid"])
        for _ in range(25):
            result = ctr.exec_run(
                ["supervisorctl", "-c", "/etc/supervisor/conf.d/supervisord.conf", "restart", "charon"]
            )
            if result.exit_code == 0:
                break
            time.sleep(0.2)

        pid = self._pid(node.container)
        park_pid = self._pid(park.name)
        for iface in node.ifaces:
            self._sh(["nsenter", "-t", str(park_pid), "-n", "ip", "link", "set", iface, "netns", str(pid)])

        park.remove(force=True)

        ctr.exec_run(["sh", "-c", "sh /etc/maeto/boot.sh > /var/log/maeto-boot.log 2>&1"], detach=True)

    def _parked(self) -> List[str]:
        prefix = "maeto-park-"
        return [
            ctr.name[len(prefix):]
            for ctr in self.client.containers.list(all=True, filters={"name": prefix})
            if ctr.name.startswith(prefix)
        ]

    async def parked(self) -> List[str]:
        return await asyncio.to_thread(self._parked)

    async def crash(self, node: Node, how: str):
        await asyncio.to_thread(self._crash, node, how)
        log.info("%s crashed (%s)", node.name, how)

    async def boot(self, node: Node):
        await asyncio.to_thread(self._boot, node)
        log.info("%s booting", node.name)
