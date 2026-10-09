# Turns a scenario file into a list of flows between lab sites

import itertools

from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, List, Optional

import yaml

from errors import ScenarioError
from lab import Site

SCENARIO_DIR = Path(__file__).resolve().parent / "scenarios"

DEFAULTS = {
    "protocol": "udp",
    "length": 1200,
    "streams": 1,
    "start": 0,
    "dscp": None,
}

DEFAULT_DURATION = 60

RATE_UNITS = {
    "k": 1_000,
    "m": 1_000_000,
    "g": 1_000_000_000,
}


@dataclass
class Flow:
    name: str
    src: Site
    dst: Site
    port: int
    protocol: str
    rate_bps: int
    streams: int
    length: int
    start: float
    duration: int
    dscp: Optional[str]

    status: str = "waiting"
    bps: float = 0.0
    error: str = ""
    end: dict = field(default_factory=dict)


def parse_rate(value) -> int:
    if isinstance(value, (int, float)):
        return int(value)

    text = str(value).strip().lower()
    multiplier = 1
    if text and text[-1] in RATE_UNITS:
        multiplier = RATE_UNITS[text[-1]]
        text = text[:-1]

    try:
        return int(float(text) * multiplier)
    except ValueError:
        raise ScenarioError(f"bad rate {value!r}, want e.g. 50M, 500k, 1G")


def find_scenario(name: str) -> Path:
    path = Path(name)
    if path.is_file():
        return path

    for candidate in [SCENARIO_DIR / name, SCENARIO_DIR / f"{name}.yml", SCENARIO_DIR / f"{name}.yaml"]:
        if candidate.is_file():
            return candidate

    raise ScenarioError(f"scenario {name!r} not found in {SCENARIO_DIR}")


def load_scenario(name: str) -> dict:
    scenario = yaml.safe_load(find_scenario(name).read_text())
    if scenario is None:
        return {}
    return scenario


def get_site(sites: Dict[str, Site], cpe) -> Site:
    cpe = str(cpe)
    if cpe not in sites:
        known = ", ".join(sorted(sites))
        raise ScenarioError(f"unknown site {cpe!r}, have {known}")
    return sites[cpe]


def mesh_pairs(sites: Dict[str, Site], tenant_id: int) -> list:
    members = []
    for site in sites.values():
        if site.tenant == tenant_id:
            members.append(site)
    members.sort(key=lambda s: s.cpe)

    if len(members) < 2:
        raise ScenarioError(f"tenant {tenant_id} has fewer than two sites")

    return list(itertools.permutations(members, 2))


def direct_pair(sites: Dict[str, Site], spec: dict) -> tuple:
    if "from" not in spec or "to" not in spec:
        raise ScenarioError("a flow needs from/to or mesh")

    src = get_site(sites, spec["from"])
    dst = get_site(sites, spec["to"])

    if src.cpe == dst.cpe:
        raise ScenarioError(f"{src.cpe} can't send to itself")

    if src.tenant != dst.tenant:
        raise ScenarioError(
            f"{src.cpe} (tenant {src.tenant}) and {dst.cpe} (tenant {dst.tenant}) can't reach each other"
        )

    return (src, dst)


def make_flow(name: str, src: Site, dst: Site, spec: dict, port: int) -> Flow:
    protocol = str(spec["protocol"]).lower()
    if protocol not in ("udp", "tcp"):
        raise ScenarioError(f"{name}: protocol must be udp or tcp")

    streams = int(spec["streams"])
    if streams < 1:
        raise ScenarioError(f"{name}: streams must be at least 1")

    return Flow(
        name=name,
        src=src,
        dst=dst,
        port=port,
        protocol=protocol,
        rate_bps=parse_rate(spec["rate"]),
        streams=streams,
        length=int(spec["length"]),
        start=float(spec["start"]),
        duration=int(spec["duration"]),
        dscp=spec["dscp"],
    )


def build_flows(scenario: dict, sites: Dict[str, Site], base_port: int) -> List[Flow]:
    defaults = dict(DEFAULTS)
    defaults["duration"] = scenario.get("duration", DEFAULT_DURATION)
    defaults.update(scenario.get("defaults") or {})

    pairs = []
    for i, entry in enumerate(scenario.get("flows") or []):
        spec = dict(defaults)
        spec.update(entry)

        if "rate" not in spec:
            raise ScenarioError(f"flow #{i}: rate is required")

        try:
            if "mesh" in spec:
                for src, dst in mesh_pairs(sites, int(spec["mesh"])):
                    pairs.append((src, dst, spec))
            else:
                src, dst = direct_pair(sites, spec)
                pairs.append((src, dst, spec))
        except ScenarioError as e:
            raise ScenarioError(f"flow #{i}: {e}")

    if not pairs:
        raise ScenarioError("scenario has no flows")

    flows = []
    name_count = {}
    for i, (src, dst, spec) in enumerate(pairs):
        name = f"{src.cpe}->{dst.cpe}"
        name_count[name] = name_count.get(name, 0) + 1
        if name_count[name] > 1:
            name = f"{name}#{name_count[name]}"

        flows.append(make_flow(name, src, dst, spec, base_port + i))

    return flows
