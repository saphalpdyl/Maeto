# Reads the generated build: which core links exist, which interface each end
# sits on, and the tc chain the generator shaped each interface with at deploy

import json
import re
import shlex

from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, List, Optional

import yaml

NETEM_DELAY = re.compile(r"netem delay ([\d.]+)ms")
TBF_RATE = re.compile(r"tbf rate ([\d.]+)mbit")


@dataclass
class Shape:
    commands: List[List[str]] = field(default_factory=list)
    delay_ms: float = 0.0
    rate_mbps: Optional[float] = None


@dataclass
class End:
    pop: str
    node: str
    iface: str
    container: str
    shape: Shape


@dataclass
class Link:
    id: str
    label: str
    a: End
    b: End
    bandwidth_mbps: Optional[float]

    def ends(self, direction: str) -> List[End]:
        if direction == "ab":
            return [self.a]
        if direction == "ba":
            return [self.b]
        return [self.a, self.b]


@dataclass
class Lab:
    name: str
    build: Path
    links: Dict[str, Link]
    pops: List[str]

    def find(self, ref: str) -> List[Link]:
        # "A-B" is every member between A and B, "A-B#2" just the second
        pair, _, member = ref.partition("#")
        a, _, b = pair.partition("-")
        found = [link for link in self.links.values() if {link.a.pop, link.b.pop} == {a, b}]
        if member:
            found = [link for link in found if link.id.endswith(member)]
        return found


def find_build(root: Path) -> Path:
    state = json.loads((root / ".state/latest.json").read_text())
    return root / state["output"]


def shapes(topology: dict) -> Dict[tuple, Shape]:
    found = {}
    for node, spec in topology["topology"]["nodes"].items():
        for line in spec.get("exec") or []:
            if not line.startswith("tc qdisc add dev "):
                continue

            command = shlex.split(line)
            shape = found.setdefault((node, command[4]), Shape())
            shape.commands.append(command)

            delay = NETEM_DELAY.search(line)
            if delay:
                shape.delay_ms = float(delay.group(1))

            rate = TBF_RATE.search(line)
            if rate:
                shape.rate_mbps = float(rate.group(1))

    return found


def load(root: Path) -> Lab:
    build = find_build(root)
    data = json.loads((build / "topology.data.json").read_text())
    topology = yaml.safe_load((build / "topology.yml").read_text())
    shaped = shapes(topology)

    def end(spec: dict) -> End:
        return End(
            pop=spec["id"],
            node=spec["node"],
            iface=spec["interface"],
            container=f"clab-{data['name']}-{spec['node']}",
            shape=shaped.get((spec["node"], spec["interface"]), Shape()),
        )

    core = [link for link in data["links"] if link["type"] == "core"]
    pairs = {}
    for spec in core:
        pair = tuple(sorted((spec["a"]["id"], spec["b"]["id"])))
        pairs[pair] = pairs.get(pair, 0) + 1

    links = {}
    seen = {}
    for spec in core:
        a, b = end(spec["a"]), end(spec["b"])
        pair = tuple(sorted((a.pop, b.pop)))
        seen[pair] = seen.get(pair, 0) + 1
        member = seen[pair]

        label = f"{a.pop}–{b.pop}"
        if pairs[pair] > 1:
            label += f" #{member}"

        link_id = f"{a.pop}{b.pop}{member}"
        links[link_id] = Link(
            id=link_id,
            label=label,
            a=a,
            b=b,
            bandwidth_mbps=spec.get("bandwidth_mbps"),
        )

    pops = sorted(pop["id"] for pop in data["pops"])
    return Lab(name=data["name"], build=build, links=links, pops=pops)
