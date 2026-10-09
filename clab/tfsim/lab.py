# Reads the generated build to find which tenant hosts exist and where they are

import json

from dataclasses import dataclass
from pathlib import Path
from typing import Dict

from errors import ScenarioError

REPO_ROOT = Path(__file__).resolve().parents[2]


@dataclass
class Site:
    cpe: str
    tenant: int
    attach: str
    container_name: str
    address: str


def find_build(state_path: Path) -> Path:
    state = json.loads(state_path.read_text())
    return REPO_ROOT / state["output"]


def read_json(path: Path) -> dict:
    if not path.is_file():
        raise ScenarioError(f"{path} not found; run make generate")
    return json.loads(path.read_text())


def load_sites(build: Path) -> Dict[str, Site]:
    topology = read_json(build / "topology.data.json")
    tenant_db = read_json(build / "tenant.db.json")

    if "hosts" not in topology:
        raise ScenarioError(f"{build} has no hosts; regenerate the topology")

    tenant_sites = {}
    for tenant in tenant_db["tenants"]:
        for site in tenant["sites"]:
            tenant_sites[site["cpe"]] = (tenant["id"], site)

    sites = {}
    for host in topology["hosts"]:
        cpe = host["cpe"]
        if cpe not in tenant_sites:
            continue

        tenant_id, site = tenant_sites[cpe]
        address = host["address"].split("/")[0]
        container_name = f"clab-{topology['name']}-{host['name']}"

        sites[cpe] = Site(
            cpe=cpe,
            tenant=tenant_id,
            attach=site["attach"],
            container_name=container_name,
            address=address,
        )

    return sites
