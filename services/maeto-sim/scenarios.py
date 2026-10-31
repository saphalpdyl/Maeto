# Runs one tfsim scenario at a time as a child process and keeps its latest status table

import asyncio
import logging
import signal
import sys
import time

from pathlib import Path
from typing import Callable, List, Optional

import yaml

log = logging.getLogger("sim.scenarios")

STOP_GRACE_S = 15
REPORT_LINES = 40


def catalog(directory: Path) -> List[dict]:
    found = []
    for path in sorted(directory.glob("*.yml")):
        spec = yaml.safe_load(path.read_text()) or {}
        duration = spec.get("duration") or 0
        found.append({
            "name": path.stem,
            "mode": spec.get("mode", "flows"),
            "duration_s": duration,
            "title": spec.get("title") or path.stem,
            "description": spec.get("description") or "",
        })
    return found


class Traffic:
    def __init__(self, tfsim: Path, root: Path, changed: Callable[[], None]):
        self.tfsim = tfsim
        self.root = root
        self.changed = changed
        self.names = {entry["name"] for entry in catalog(tfsim / "scenarios")}

        self.process: Optional[asyncio.subprocess.Process] = None
        self.name: Optional[str] = None
        self.owner: Optional[str] = None
        self.started_at = 0.0
        self.expires_at = 0.0
        self.report = ""
        self.last: Optional[dict] = None

    @property
    def running(self) -> bool:
        return self.process is not None and self.process.returncode is None

    async def start(self, name: str, max_s: float, owner: str = "manual"):
        if name not in self.names:
            raise ValueError(f"unknown scenario {name!r}")
        if self.running:
            raise ValueError(f"{self.name} is already running; stop it first")

        self.process = await asyncio.create_subprocess_exec(
            sys.executable, "-u", str(self.tfsim / "main.py"),
            "-s", name, "--state", str(self.root / ".state/latest.json"), "--interval", "5",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.STDOUT,
        )
        self.name = name
        self.owner = owner
        self.started_at = time.time()
        self.expires_at = self.started_at + max_s
        self.report = ""
        asyncio.create_task(self._follow(self.process))
        log.info("scenario %s started by %s", name, owner)
        self.changed()

    async def _follow(self, process: asyncio.subprocess.Process):
        block: List[str] = []
        tail: List[str] = []

        async for raw in process.stdout:
            line = raw.decode(errors="replace").rstrip()
            tail = (tail + [line])[-REPORT_LINES:]
            if line.strip():
                block.append(line)
            elif block:
                self.report = "\n".join(block[-REPORT_LINES:])
                block = []
                self.changed()

        code = await process.wait()
        if process is not self.process:
            return

        if code != 0 and not self.report:
            self.report = "\n".join(tail)

        self.last = {
            "name": self.name,
            "exit_code": code,
            "ended_at": time.time(),
            "report": self.report,
        }
        log.info("scenario %s exited with %s", self.name, code)
        self.process = None
        self.name = None
        self.owner = None
        self.changed()

    def stop(self):
        if not self.running:
            return
        # tfsim cleans up its iperf3 processes on ctrl-c
        self.process.send_signal(signal.SIGINT)
        asyncio.create_task(self._kill_late(self.process))

    async def _kill_late(self, process: asyncio.subprocess.Process):
        try:
            await asyncio.wait_for(process.wait(), STOP_GRACE_S)
        except asyncio.TimeoutError:
            process.kill()

    def expired(self) -> bool:
        return self.running and time.time() >= self.expires_at

    def view(self) -> Optional[dict]:
        if not self.running:
            return None
        return {
            "name": self.name,
            "owner": self.owner,
            "started_at": self.started_at,
            "expires_at": self.expires_at,
            "report": self.report,
        }
