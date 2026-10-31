# What a demo visitor may do to a link, and the bounds that keep the testbed usable

import time

from dataclasses import dataclass, field
from typing import Dict, List, Optional

from lab import Lab

DIRECTIONS = ("both", "ab", "ba")

LIMITS = {
    "delay_ms": 200,
    "jitter_ms": 50,
    "loss_pct": 30,
    "rate_mbps": 1000,
    "ttl_min_s": 15,
    "ttl_max_s": 600,
    "ttl_default_s": 120,
    "manual_faults": 4,
    "manual_down": 2,
    "scenario_max_s": 900,
}


class FaultError(Exception):
    pass


@dataclass
class Impairment:
    delay_ms: float = 0
    jitter_ms: float = 0
    loss_pct: float = 0
    rate_mbps: Optional[float] = None


@dataclass
class Fault:
    link: str
    kind: str
    source: str
    expires_at: float
    direction: str = "both"
    impairment: Impairment = field(default_factory=Impairment)
    started_at: float = field(default_factory=time.time)

    def view(self) -> dict:
        view = {
            "link": self.link,
            "kind": self.kind,
            "source": self.source,
            "direction": self.direction,
            "started_at": self.started_at,
            "expires_at": self.expires_at,
        }
        if self.kind == "impair":
            view.update(vars(self.impairment))
        return view


def number(params: dict, key: str, high: float, low: float = 0) -> float:
    value = params.get(key)
    if value in (None, ""):
        return 0
    try:
        value = float(value)
    except (TypeError, ValueError):
        raise FaultError(f"{key} must be a number")
    if value < low or value > high:
        raise FaultError(f"{key} must be between {low:g} and {high:g}")
    return value


def impairment(params: dict) -> Impairment:
    rate = number(params, "rate_mbps", LIMITS["rate_mbps"], low=0)
    parsed = Impairment(
        delay_ms=number(params, "delay_ms", LIMITS["delay_ms"]),
        jitter_ms=number(params, "jitter_ms", LIMITS["jitter_ms"]),
        loss_pct=number(params, "loss_pct", LIMITS["loss_pct"]),
        rate_mbps=rate or None,
    )
    if parsed.rate_mbps is not None and parsed.rate_mbps < 1:
        raise FaultError("rate_mbps must be at least 1")
    if not (parsed.delay_ms or parsed.jitter_ms or parsed.loss_pct or parsed.rate_mbps):
        raise FaultError("set at least one of delay, jitter, loss or rate")
    return parsed


def ttl(params: dict) -> float:
    value = params.get("ttl_s")
    if value in (None, ""):
        return LIMITS["ttl_default_s"]
    return number(params, "ttl_s", LIMITS["ttl_max_s"], low=LIMITS["ttl_min_s"])


def direction(params: dict) -> str:
    value = params.get("direction") or "both"
    if value not in DIRECTIONS:
        raise FaultError(f"direction must be one of {', '.join(DIRECTIONS)}")
    return value


def connected(lab: Lab, down: List[str]) -> bool:
    neighbours: Dict[str, set] = {pop: set() for pop in lab.pops}
    for link in lab.links.values():
        if link.id in down:
            continue
        neighbours[link.a.pop].add(link.b.pop)
        neighbours[link.b.pop].add(link.a.pop)

    start = lab.pops[0]
    reached = {start}
    queue = [start]
    while queue:
        for peer in neighbours[queue.pop()]:
            if peer not in reached:
                reached.add(peer)
                queue.append(peer)

    return len(reached) == len(lab.pops)


def check_manual(lab: Lab, faults: Dict[str, Fault], link: str, kind: str):
    others = {key: fault for key, fault in faults.items() if key != link}
    manual = [fault for fault in others.values() if fault.source == "manual"]

    if len(manual) >= LIMITS["manual_faults"]:
        raise FaultError(f"at most {LIMITS['manual_faults']} links can be faulted at once; clear one first")

    if kind != "down":
        return

    if len([fault for fault in manual if fault.kind == "down"]) >= LIMITS["manual_down"]:
        raise FaultError(f"at most {LIMITS['manual_down']} links can be down at once")

    down = [key for key, fault in others.items() if fault.kind == "down"] + [link]
    if not connected(lab, down):
        raise FaultError("taking this link down would cut a PoP off the fabric")
