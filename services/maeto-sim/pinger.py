# Runs iputils ping inside a lab host for the length of a story step and keeps
# the last replies and losses, so the pane can show traffic dropping out and
# coming back while a fault is in place.

import asyncio
import logging
import os
import re
import signal
import time

from collections import deque
from typing import Callable, Optional

import docker

from lab import Host

log = logging.getLogger("sim.ping")

INTERVAL_S = 0.2
KEEP = 150

REPLY = re.compile(r"icmp_seq=(\d+) .*time=([\d.]+) ms")
LOST = re.compile(r"no answer yet for icmp_seq=(\d+)")


class Pinger:
    def __init__(self, client: docker.DockerClient, changed: Callable[[], None]):
        self.client = client
        self.changed = changed
        self.source: Optional[Host] = None
        self.target: Optional[Host] = None
        self.owner: Optional[str] = None
        self.started_at = 0.0
        self.samples: deque = deque(maxlen=KEEP)
        self.sent = 0
        self.lost = 0
        self.exec_id: Optional[str] = None
        self.task: Optional[asyncio.Task] = None

    @property
    def running(self) -> bool:
        return self.task is not None and not self.task.done()

    def _record(self, line: str):
        reply = REPLY.search(line)
        if reply:
            seq, rtt = int(reply.group(1)), float(reply.group(2))
            for sample in self.samples:
                if sample["seq"] == seq:
                    if sample["rtt_ms"] is None:
                        self.lost -= 1
                    sample["rtt_ms"] = rtt
                    return
            self.samples.append({"seq": seq, "rtt_ms": rtt})
            self.sent += 1
            return

        lost = LOST.search(line)
        if lost:
            self.samples.append({"seq": int(lost.group(1)), "rtt_ms": None})
            self.sent += 1
            self.lost += 1

    def _apply(self, lines):
        for line in lines:
            self._record(line)
        self.changed()

    def _run(self, loop: asyncio.AbstractEventLoop, hold_s: float):
        api = self.client.api
        command = ["ping", "-6", "-n", "-O", "-i", str(INTERVAL_S), "-w", str(int(hold_s)), self.target.address]
        self.exec_id = api.exec_create(self.source.container, command, tty=True)["Id"]

        buffer = ""
        for chunk in api.exec_start(self.exec_id, stream=True):
            buffer += chunk.decode(errors="replace").replace("\r", "")
            *lines, buffer = buffer.split("\n")
            if lines:
                loop.call_soon_threadsafe(self._apply, lines)

    async def start(self, source: Host, target: Host, hold_s: float, owner: str):
        await self.stop()
        self.source, self.target, self.owner = source, target, owner
        self.started_at = time.time()
        self.samples.clear()
        self.sent = 0
        self.lost = 0

        loop = asyncio.get_running_loop()
        self.task = asyncio.create_task(asyncio.to_thread(self._run, loop, hold_s))
        log.info("pinging %s from %s for %.0fs", target.name, source.name, hold_s)
        self.changed()

    async def stop(self):
        if self.exec_id:
            try:
                info = self.client.api.exec_inspect(self.exec_id)
                if info.get("Running") and info.get("Pid"):
                    os.kill(info["Pid"], signal.SIGINT)
            except (docker.errors.APIError, ProcessLookupError) as e:
                log.debug("stopping ping: %s", e)
            self.exec_id = None

        if self.task:
            await asyncio.gather(self.task, return_exceptions=True)
            self.task = None
            self.changed()

    def longest_gap_ms(self) -> int:
        longest = run = 0
        for sample in self.samples:
            run = run + 1 if sample["rtt_ms"] is None else 0
            longest = max(longest, run)
        return int(longest * INTERVAL_S * 1000)

    def view(self) -> Optional[dict]:
        if self.source is None:
            return None
        return {
            "from": self.source.name,
            "to": self.target.name,
            "running": self.running,
            "interval_ms": int(INTERVAL_S * 1000),
            "keep": KEEP,
            "sent": self.sent,
            "lost": self.lost,
            "longest_gap_ms": self.longest_gap_ms(),
            "samples": list(self.samples),
        }
