# Canned stories: a timed list of faults (and optionally traffic) that tells one
# thing about the control plane, reverted as a whole when it ends or is stopped

from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List

import yaml

from lab import Lab

KINDS = ("impair", "down", "traffic", "crash", "ping")


class StoryError(Exception):
    pass


@dataclass
class Step:
    at: float
    kind: str
    target: object
    hold_s: float


@dataclass
class Story:
    name: str
    title: str
    description: str
    duration_s: float
    steps: List[Step]

    def view(self) -> dict:
        return {
            "name": self.name,
            "title": self.title,
            "description": self.description,
            "duration_s": self.duration_s,
        }


def parse(name: str, spec: dict, lab: Lab, scenarios: set) -> Story:
    duration = float(spec["duration"])
    steps = []

    for raw in spec.get("steps") or []:
        kinds = [kind for kind in KINDS if kind in raw]
        if len(kinds) != 1:
            raise StoryError(f"{name}: each step needs exactly one of {', '.join(KINDS)}")

        kind = kinds[0]
        target = raw[kind]
        at = float(raw.get("at", 0))

        if kind == "traffic":
            if target not in scenarios:
                raise StoryError(f"{name}: unknown scenario {target!r}")
        elif kind == "ping":
            for end in ("from", "to"):
                if target.get(end) not in lab.hosts:
                    raise StoryError(f"{name}: no host {target.get(end)!r} in this topology")
        elif kind == "crash":
            ref = target["node"] if isinstance(target, dict) else target
            if not lab.node(ref):
                raise StoryError(f"{name}: no node {ref!r} in this topology")
        elif isinstance(target, dict) and target.get("on_path_of") == "ping":
            if not any("ping" in step for step in spec.get("steps") or []):
                raise StoryError(f"{name}: on_path_of: ping needs a ping step")
        else:
            ref = target["link"] if isinstance(target, dict) else target
            if not lab.find(ref):
                raise StoryError(f"{name}: no link {ref!r} in this topology")

        steps.append(Step(at=at, kind=kind, target=target, hold_s=float(raw.get("for", duration - at))))

    return Story(
        name=name,
        title=spec.get("title") or name,
        description=spec.get("description") or "",
        duration_s=duration,
        steps=sorted(steps, key=lambda step: step.at),
    )


def load(directory: Path, lab: Lab, scenarios: set, traffic: bool) -> Dict[str, Story]:
    stories = {}
    for path in sorted(directory.glob("*.yml")):
        spec = yaml.safe_load(path.read_text())
        if not traffic and any("traffic" in step for step in spec.get("steps") or []):
            continue
        stories[path.stem] = parse(path.stem, spec, lab, scenarios)
    return stories
