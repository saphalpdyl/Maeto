# Customer-like random traffic. Sessions arrive as a Poisson process whose rate
# follows a business day and drifts with a slow log-normal noise. Transfer sizes
# are Pareto/log-normal, call and stream lengths log-normal, so a few elephants
# carry most of the bytes. Content flows from the big sites (hubs) to the small
# ones (branches), backups flow back, calls go both ways at different rates.

import math
import random

from dataclasses import dataclass
from typing import Dict, List

from errors import ScenarioError
from lab import Site
from scenario import Flow

# load at each hour of the day, as a share of the busiest hour
DAY_PROFILE = [
    0.12, 0.10, 0.09, 0.08, 0.08, 0.10, 0.18, 0.38,
    0.70, 0.92, 1.00, 0.97, 0.82, 0.90, 0.98, 0.95,
    0.85, 0.66, 0.45, 0.34, 0.28, 0.22, 0.18, 0.14,
]

APPS = ["web", "bulk", "backup", "streaming", "conference", "voice"]

DEFAULT_MIX = {
    "web": 0.50,
    "bulk": 0.08,
    "backup": 0.04,
    "streaming": 0.12,
    "conference": 0.06,
    "voice": 0.20,
}

VIDEO_LADDER = [1_500_000, 3_000_000, 4_500_000, 6_000_000, 8_000_000, 16_000_000]
VIDEO_LADDER_WEIGHTS = [2, 4, 5, 4, 2, 1]

VOICE_DSCP = 46
VIDEO_DSCP = 34

NOISE_SIGMA = 0.4
NOISE_SECONDS = 60
NOISE_CEILING = 3.0

HUB_SHARE = 0.8

# a byte-sized transfer never runs longer than this at its rate
MAX_TRANSFER_SECONDS = 600


@dataclass
class Config:
    duration: float
    day: float
    start_hour: float
    sessions: float
    max_flows: int
    scale: float
    seed: int
    tenants: Dict[int, List[Site]]
    mix: Dict[str, float]


def load_config(scenario: dict, sites: Dict[str, Site]) -> Config:
    by_tenant = {}
    for site in sorted(sites.values(), key=lambda s: s.cpe):
        by_tenant.setdefault(site.tenant, []).append(site)

    wanted = scenario.get("tenants") or sorted(by_tenant)
    tenants = {}
    for tenant_id in wanted:
        tenant_id = int(tenant_id)
        members = by_tenant.get(tenant_id, [])
        if len(members) < 2:
            raise ScenarioError(f"tenant {tenant_id} has fewer than two sites")
        tenants[tenant_id] = members

    mix = dict(DEFAULT_MIX)
    mix.update(scenario.get("mix") or {})
    for app in mix:
        if app not in APPS:
            raise ScenarioError(f"unknown app {app!r} in mix, have {', '.join(APPS)}")

    seed = scenario.get("seed")
    if seed is None:
        seed = random.SystemRandom().randrange(2**32)

    config = Config(
        duration=float(scenario.get("duration") or 0),
        day=float(scenario.get("day", 1800)),
        start_hour=float(scenario.get("start_hour", 8)),
        sessions=float(scenario.get("sessions", 1.0)),
        max_flows=int(scenario.get("max_flows", 96)),
        scale=float(scenario.get("scale", 1.0)),
        seed=int(seed),
        tenants=tenants,
        mix=mix,
    )

    if config.day <= 0 or config.sessions <= 0 or config.max_flows < 2 or config.scale <= 0:
        raise ScenarioError("day, sessions and scale must be positive and max_flows at least 2")

    return config


class Load:
    def __init__(self, config: Config, rng: random.Random):
        self.config = config
        self.rng = rng
        self.noise = 0.0

    def hour(self, elapsed: float) -> float:
        return (self.config.start_hour + elapsed / self.config.day * 24) % 24

    def share(self, elapsed: float) -> float:
        hour = self.hour(elapsed)
        index = int(hour)
        before = DAY_PROFILE[index]
        after = DAY_PROFILE[(index + 1) % 24]
        return before + (after - before) * (hour - index)

    # log of the noise is an Ornstein-Uhlenbeck walk: busy and quiet spells
    # that last about a minute rather than independent jitter every second
    def drift(self, seconds: float):
        keep = math.exp(-seconds / NOISE_SECONDS)
        fresh = math.sqrt(1 - keep * keep) * NOISE_SIGMA * self.rng.gauss(0, 1)
        self.noise = keep * self.noise + fresh

    def factor(self) -> float:
        return min(math.exp(self.noise - NOISE_SIGMA * NOISE_SIGMA / 2), NOISE_CEILING)

    def rate(self, elapsed: float) -> float:
        return self.config.sessions * self.share(elapsed) * self.factor()

    def ceiling(self) -> float:
        return self.config.sessions * NOISE_CEILING


def lognormal(rng: random.Random, median: float, sigma: float, low: float, high: float) -> float:
    value = median * math.exp(rng.gauss(0, sigma))
    return min(max(value, low), high)


def pareto(rng: random.Random, minimum: float, alpha: float, high: float) -> float:
    return min(minimum * rng.paretovariate(alpha), high)


def weighted(rng: random.Random, items: list, weights: list):
    return rng.choices(items, weights=weights, k=1)[0]


def pick_app(rng: random.Random, config: Config) -> str:
    apps = list(config.mix)
    return weighted(rng, apps, [config.mix[app] for app in apps])


def pick_tenant(rng: random.Random, config: Config) -> List[Site]:
    tenants = list(config.tenants.values())
    sizes = [sum(site.reservation_mbps for site in members) for members in tenants]
    return weighted(rng, tenants, sizes)


# the tenant's biggest site is its data centre and serves most of the content;
# the rest of the time it is some other site, picked by size like the branches
def pick_hub_and_branch(rng: random.Random, members: List[Site]):
    hub = max(members, key=lambda site: site.reservation_mbps)
    if rng.random() > HUB_SHARE:
        hub = weighted(rng, members, [site.reservation_mbps for site in members])

    others = [site for site in members if site is not hub]
    branch = weighted(rng, others, [site.reservation_mbps for site in others])
    return hub, branch


def pick_peers(rng: random.Random, members: List[Site]):
    first = weighted(rng, members, [site.reservation_mbps for site in members])
    others = [site for site in members if site is not first]
    second = weighted(rng, others, [site.reservation_mbps for site in others])
    return first, second


def marking(site: Site, app_class: str, dscp: int):
    if app_class in site.classes:
        return dscp
    return None


def leg(app: str, src: Site, dst: Site, protocol: str, rate_bps: float, length: int = 1200,
        dscp=None, size=None, duration: float = 0) -> Flow:
    rate_bps = int(rate_bps)
    if size is not None:
        size = int(min(size, rate_bps / 8 * MAX_TRANSFER_SECONDS))

    return Flow(
        name=f"{app} {src.cpe}->{dst.cpe}",
        src=src,
        dst=dst,
        port=0,
        protocol=protocol,
        rate_bps=rate_bps,
        streams=1,
        length=length,
        start=0,
        duration=max(int(duration), 1),
        dscp=dscp,
        app=app,
        size=size,
    )


# a page, an API call, a mail with attachments: mostly down to the branch
def web(rng: random.Random, members: List[Site], scale: float) -> List[Flow]:
    hub, branch = pick_hub_and_branch(rng, members)
    src, dst = hub, branch
    if rng.random() < 0.15:
        src, dst = branch, hub

    size = lognormal(rng, 1_000_000, 1.5, 100_000, 300_000_000)
    rate = lognormal(rng, 20_000_000, 0.8, 2_000_000, 200_000_000) * scale
    return [leg("web", src, dst, "tcp", rate, size=size)]


# file shares and software rollouts pushed out from the hub
def bulk(rng: random.Random, members: List[Site], scale: float) -> List[Flow]:
    hub, branch = pick_hub_and_branch(rng, members)
    size = pareto(rng, 10_000_000, 1.2, 4_000_000_000)
    rate = lognormal(rng, 60_000_000, 0.7, 10_000_000, 400_000_000) * scale
    return [leg("bulk", hub, branch, "tcp", rate, size=size)]


def backup(rng: random.Random, members: List[Site], scale: float) -> List[Flow]:
    hub, branch = pick_hub_and_branch(rng, members)
    size = pareto(rng, 50_000_000, 1.3, 8_000_000_000)
    rate = lognormal(rng, 100_000_000, 0.5, 20_000_000, 500_000_000) * scale
    return [leg("backup", branch, hub, "tcp", rate, size=size)]


# half of it rides QUIC, so UDP
def streaming(rng: random.Random, members: List[Site], scale: float) -> List[Flow]:
    hub, branch = pick_hub_and_branch(rng, members)
    protocol = weighted(rng, ["udp", "tcp"], [1, 1])
    rate = weighted(rng, VIDEO_LADDER, VIDEO_LADDER_WEIGHTS) * scale
    duration = lognormal(rng, 90, 1.0, 10, 1200)
    dscp = marking(hub, "streaming", VIDEO_DSCP)
    return [leg("streaming", hub, branch, protocol, rate, dscp=dscp, duration=duration)]


# the side presenting sends more than it gets back
def conference(rng: random.Random, members: List[Site], scale: float) -> List[Flow]:
    first, second = pick_peers(rng, members)
    duration = lognormal(rng, 120, 0.8, 20, 1800)
    there = lognormal(rng, 2_500_000, 0.4, 500_000, 8_000_000) * scale
    back = lognormal(rng, 1_000_000, 0.5, 300_000, 4_000_000) * scale

    return [
        leg("conference", first, second, "udp", there, length=1100,
            dscp=marking(first, "streaming", VIDEO_DSCP), duration=duration),
        leg("conference", second, first, "udp", back, length=1100,
            dscp=marking(second, "streaming", VIDEO_DSCP), duration=duration),
    ]


def voice(rng: random.Random, members: List[Site], scale: float) -> List[Flow]:
    first, second = pick_peers(rng, members)
    duration = lognormal(rng, 45, 1.0, 5, 1800)
    rate = 100_000 * scale

    return [
        leg("voice", first, second, "udp", rate, length=200,
            dscp=marking(first, "voice", VOICE_DSCP), duration=duration),
        leg("voice", second, first, "udp", rate, length=200,
            dscp=marking(second, "voice", VOICE_DSCP), duration=duration),
    ]


SESSIONS = {
    "web": web,
    "bulk": bulk,
    "backup": backup,
    "streaming": streaming,
    "conference": conference,
    "voice": voice,
}


def new_session(rng: random.Random, config: Config) -> List[Flow]:
    app = pick_app(rng, config)
    members = pick_tenant(rng, config)
    return SESSIONS[app](rng, members, config.scale)
