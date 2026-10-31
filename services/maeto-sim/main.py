# maeto-sim: lets demo visitors break the testbed on purpose. Takes commands on
# maeto.sim.cmd (request/reply), applies them to the lab with tc/ip, reverts
# every fault on its own timer, and publishes what is going on to the maeto-sim
# KV bucket for the pane to watch.

import asyncio
import json
import logging
import os
import time

from pathlib import Path
from typing import Dict, Optional

import docker
import nats

from nats.js.errors import BucketNotFoundError

import faults
import lab as lablib
import scenarios
import stories as storylib

from faults import Fault, FaultError, LIMITS
from shaping import Shaper, ShapingError

log = logging.getLogger("sim")

ROOT = Path(os.environ.get("MAETO_ROOT", "/app"))
TFSIM = Path(os.environ.get("TFSIM_DIR", ROOT / "clab/tfsim"))
STORIES = Path(__file__).resolve().parent / "stories"
NATS_URL = os.environ.get("NATS_CONNECT_URL", "nats://127.0.0.1:4222")

SUBJECT = "maeto.sim.cmd"
BUCKET = "maeto-sim"
KEY = "state"
HEARTBEAT_S = 2


class Sim:
    def __init__(self, shaper: Shaper):
        self.shaper = shaper
        self.lock = asyncio.Lock()
        self.faults: Dict[str, Fault] = {}
        self.traffic = scenarios.Traffic(TFSIM, ROOT, self.changed)
        self.dirty = asyncio.Event()

        self.lab: Optional[lablib.Lab] = None
        self.stories: Dict[str, storylib.Story] = {}
        self.story: Optional[storylib.Story] = None
        self.story_task: Optional[asyncio.Task] = None
        self.story_started_at = 0.0
        self.error: Optional[str] = None

    def changed(self):
        self.dirty.set()

    def load_lab(self):
        try:
            self.lab = lablib.load(ROOT)
            self.stories = storylib.load(STORIES, self.lab, self.traffic.names)
            self.error = None
        except (OSError, KeyError, ValueError, storylib.StoryError) as e:
            log.error("cannot load the lab: %s", e)
            self.lab = None
            self.stories = {}
            self.error = f"cannot load the lab build: {e}"

    def link(self, link_id: str) -> lablib.Link:
        if self.lab is None:
            raise FaultError(self.error or "the lab is not loaded")
        if link_id not in self.lab.links:
            raise FaultError(f"unknown link {link_id!r}")
        return self.lab.links[link_id]

    async def revert(self, fault: Fault):
        link = self.lab.links.get(fault.link) if self.lab else None
        if link is None:
            return

        if fault.kind == "down":
            for end in link.ends("both"):
                await self.shaper.set_up(end, True)
        else:
            for end in link.ends(fault.direction):
                await self.shaper.restore(end)

    async def apply(self, fault: Fault):
        link = self.link(fault.link)

        previous = self.faults.pop(fault.link, None)
        if previous:
            await self.revert(previous)

        try:
            if fault.kind == "down":
                for end in link.ends("both"):
                    await self.shaper.set_up(end, False)
            else:
                imp = fault.impairment
                for end in link.ends(fault.direction):
                    await self.shaper.impair(end, imp.delay_ms, imp.jitter_ms, imp.loss_pct, imp.rate_mbps)
        except ShapingError:
            try:
                await self.revert(fault)
            except ShapingError as e:
                log.error("rolling back %s on %s: %s", fault.kind, link.label, e)
            raise

        self.faults[fault.link] = fault
        log.info("%s %s on %s for %.0fs", fault.source, fault.kind, link.label, fault.expires_at - time.time())
        self.changed()

    async def clear(self, link_id: str):
        fault = self.faults.pop(link_id, None)
        if fault:
            await self.revert(fault)
            log.info("cleared %s on %s", fault.kind, link_id)
            self.changed()

    async def clear_source(self, source: str):
        for link_id in [key for key, fault in self.faults.items() if fault.source == source]:
            try:
                await self.clear(link_id)
            except ShapingError as e:
                log.error("reverting %s: %s", link_id, e)

    async def reset(self):
        self.traffic.stop()
        self.faults.clear()
        self.load_lab()

        if self.lab is None:
            return

        for link in self.lab.links.values():
            for end in link.ends("both"):
                try:
                    await self.shaper.set_up(end, True)
                    await self.shaper.restore(end)
                except ShapingError as e:
                    log.warning("reset %s: %s", link.label, e)
        log.info("testbed reset")
        self.changed()

    async def start_story(self, name: str):
        if name not in self.stories:
            raise FaultError(f"unknown story {name!r}")
        if self.story:
            raise FaultError(f"{self.story.title} is already running; stop it first")

        self.story = self.stories[name]
        self.story_started_at = time.time()
        self.story_task = asyncio.create_task(self.run_story(self.story))
        self.changed()

    async def run_story(self, story: storylib.Story):
        source = f"story:{story.name}"
        try:
            for step in story.steps:
                await asyncio.sleep(max(self.story_started_at + step.at - time.time(), 0))
                async with self.lock:
                    await self.story_step(story, step, source)

            await asyncio.sleep(max(self.story_started_at + story.duration_s - time.time(), 0))
        except asyncio.CancelledError:
            pass
        except (FaultError, ShapingError, ValueError) as e:
            log.error("story %s failed: %s", story.name, e)
            self.error = f"story {story.title} failed: {e}"
        finally:
            async with self.lock:
                await self.clear_source(source)
                if self.traffic.owner == source:
                    self.traffic.stop()
                self.story = None
                self.story_task = None
                self.changed()

    async def story_step(self, story: storylib.Story, step: storylib.Step, source: str):
        if step.kind == "traffic":
            if not self.traffic.running:
                await self.traffic.start(step.target, step.hold_s, owner=source)
            return

        spec = step.target if isinstance(step.target, dict) else {"link": step.target}
        for link in self.lab.find(spec["link"]):
            fault = Fault(
                link=link.id,
                kind=step.kind,
                source=source,
                expires_at=time.time() + step.hold_s,
                direction=faults.direction(spec),
            )
            if step.kind == "impair":
                fault.impairment = faults.impairment(spec)
            await self.apply(fault)

    async def stop_story(self):
        task = self.story_task
        if task:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)

    async def command(self, request: dict) -> dict:
        action = request.get("action")

        if action == "impair":
            async with self.lock:
                self.link(request.get("link"))
                faults.check_manual(self.lab, self.faults, request["link"], "impair")
                await self.apply(Fault(
                    link=request["link"],
                    kind="impair",
                    source="manual",
                    expires_at=time.time() + faults.ttl(request),
                    direction=faults.direction(request),
                    impairment=faults.impairment(request),
                ))
        elif action == "down":
            async with self.lock:
                self.link(request.get("link"))
                faults.check_manual(self.lab, self.faults, request["link"], "down")
                await self.apply(Fault(
                    link=request["link"],
                    kind="down",
                    source="manual",
                    expires_at=time.time() + faults.ttl(request),
                ))
        elif action == "clear":
            async with self.lock:
                await self.clear(request.get("link"))
        elif action == "scenario_start":
            await self.traffic.start(request.get("name"), LIMITS["scenario_max_s"])
        elif action == "scenario_stop":
            self.traffic.stop()
        elif action == "story_start":
            await self.start_story(request.get("name"))
        elif action == "story_stop":
            await self.stop_story()
        elif action == "reset":
            await self.stop_story()
            async with self.lock:
                await self.reset()
        else:
            raise FaultError(f"unknown action {action!r}")

        return {"ok": True}

    async def reap(self):
        while True:
            await asyncio.sleep(1)
            now = time.time()

            async with self.lock:
                for link_id in [key for key, fault in self.faults.items() if fault.expires_at <= now]:
                    try:
                        await self.clear(link_id)
                    except ShapingError as e:
                        log.error("reverting %s: %s", link_id, e)

            if self.traffic.expired():
                log.info("scenario %s hit its time limit", self.traffic.name)
                self.traffic.stop()

    def view(self) -> dict:
        links = []
        if self.lab:
            for link in self.lab.links.values():
                links.append({
                    "id": link.id,
                    "label": link.label,
                    "a": link.a.pop,
                    "b": link.b.pop,
                    "a_iface": link.a.iface,
                    "b_iface": link.b.iface,
                    "bandwidth_mbps": link.bandwidth_mbps,
                    "delay_ms": link.a.shape.delay_ms,
                })

        story = None
        if self.story:
            story = {
                "name": self.story.name,
                "title": self.story.title,
                "started_at": self.story_started_at,
                "ends_at": self.story_started_at + self.story.duration_s,
            }

        return {
            "updated_at": time.time(),
            "error": self.error,
            "limits": LIMITS,
            "links": links,
            "faults": [fault.view() for fault in self.faults.values()],
            "scenarios": scenarios.catalog(TFSIM / "scenarios"),
            "scenario": self.traffic.view(),
            "last_scenario": self.traffic.last,
            "stories": [story.view() for story in self.stories.values()],
            "story": story,
        }


async def publish(sim: Sim, kv):
    while True:
        try:
            await asyncio.wait_for(sim.dirty.wait(), HEARTBEAT_S)
        except asyncio.TimeoutError:
            pass
        sim.dirty.clear()

        try:
            await kv.put(KEY, json.dumps(sim.view()).encode())
        except Exception as e:
            log.warning("publishing state: %s", e)

        await asyncio.sleep(0.25)


async def bucket(nc):
    js = nc.jetstream()
    try:
        return await js.key_value(BUCKET)
    except BucketNotFoundError:
        return await js.create_key_value(bucket=BUCKET, history=1)


async def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s %(levelname)s %(message)s")

    sim = Sim(Shaper(docker.from_env()))
    async with sim.lock:
        await sim.reset()

    nc = await nats.connect(NATS_URL, max_reconnect_attempts=-1, reconnect_time_wait=2)
    kv = await bucket(nc)

    async def handle(msg):
        try:
            request = json.loads(msg.data or b"{}")
            reply = await sim.command(request)
        except (FaultError, ShapingError, ValueError, KeyError, json.JSONDecodeError) as e:
            reply = {"ok": False, "error": str(e)}
        except Exception as e:
            log.exception("command failed")
            reply = {"ok": False, "error": f"internal error: {e}"}

        if msg.reply:
            await msg.respond(json.dumps(reply).encode())

    await nc.subscribe(SUBJECT, cb=handle)
    log.info("listening on %s, %d links, %d stories", SUBJECT, len(sim.lab.links) if sim.lab else 0, len(sim.stories))

    await asyncio.gather(sim.reap(), publish(sim, kv))


if __name__ == "__main__":
    asyncio.run(main())
