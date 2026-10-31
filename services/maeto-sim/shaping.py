# Drives tc and ip inside the PoP containers. Every change tears the root qdisc
# down and rebuilds the generator's netem -> tbf chain, so the base delay and
# capacity of a link survive an impairment and come back exactly on clear.

import asyncio
import logging

from typing import List, Optional

import docker

from lab import End

log = logging.getLogger("sim.shaping")


class ShapingError(Exception):
    pass


class Shaper:
    def __init__(self, client: docker.DockerClient):
        self.client = client

    def _exec(self, container: str, command: List[str], check: bool) -> str:
        try:
            result = self.client.containers.get(container).exec_run(command)
        except docker.errors.NotFound:
            raise ShapingError(f"{container} is not running")

        output = result.output.decode(errors="replace").strip()
        if check and result.exit_code != 0:
            raise ShapingError(f"{container}: {' '.join(command)}: {output}")
        return output

    async def run(self, container: str, command: List[str], check: bool = True) -> str:
        return await asyncio.to_thread(self._exec, container, command, check)

    async def impair(self, end: End, delay_ms: float, jitter_ms: float, loss_pct: float,
                     rate_mbps: Optional[float]):
        netem = ["tc", "qdisc", "add", "dev", end.iface, "root", "handle", "1:", "netem",
                 "delay", f"{end.shape.delay_ms + delay_ms}ms"]
        if jitter_ms > 0:
            netem += [f"{jitter_ms}ms"]
        if loss_pct > 0:
            netem += ["loss", f"{loss_pct}%"]

        rate = end.shape.rate_mbps
        if rate_mbps is not None:
            rate = min(rate, rate_mbps) if rate else rate_mbps

        commands = [netem]
        if rate:
            burst = max(32, int(rate * 1000 / 800))
            commands.append(["tc", "qdisc", "add", "dev", end.iface, "parent", "1:1", "handle", "10:",
                             "tbf", "rate", f"{rate}mbit", "burst", f"{burst}kb", "latency", "50ms"])

        await self._replace(end, commands)

    async def restore(self, end: End):
        await self._replace(end, end.shape.commands)

    async def _replace(self, end: End, commands: List[List[str]]):
        await self.run(end.container, ["tc", "qdisc", "del", "dev", end.iface, "root"], check=False)
        for command in commands:
            await self.run(end.container, command)

    async def set_up(self, end: End, up: bool):
        if not up:
            # the kernel drops an interface's v6 addresses on admin down unless told not to
            await self.run(end.container, ["sysctl", "-w", f"net.ipv6.conf.{end.iface}.keep_addr_on_down=1"],
                           check=False)
        await self.run(end.container, ["ip", "link", "set", "dev", end.iface, "up" if up else "down"])
