# Traffic simulator: drives iperf3 between the lab's tenant hosts from a scenario file

import argparse
import sys

from pathlib import Path

import docker

import report
import runner

from errors import ScenarioError
from lab import REPO_ROOT, find_build, load_sites
from scenario import build_flows, load_scenario


def parse_args():
    ap = argparse.ArgumentParser(description="Traffic simulator for the maeto lab")
    ap.add_argument("-s", "--scenario", required=True, help="scenario name in clab/tfsim/scenarios or a path to a .yml")
    ap.add_argument("-d", "--docker", default="/var/run/docker.sock", help="path to the docker socket")
    ap.add_argument("--state", default=str(REPO_ROOT / ".state/latest.json"), help="generator state file pointing at the build")
    ap.add_argument("--build", help="build dir with topology.data.json and tenant.db.json; overrides --state")
    ap.add_argument("--base-port", type=int, default=5301)
    ap.add_argument("--interval", type=float, default=5, help="seconds between status tables")
    ap.add_argument("--dry-run", action="store_true", help="print the iperf3 commands and exit")
    return ap.parse_args()


def main() -> int:
    args = parse_args()

    try:
        if args.build:
            build = Path(args.build)
        else:
            build = find_build(Path(args.state))

        sites = load_sites(build)
        scenario = load_scenario(args.scenario)
        flows = build_flows(scenario, sites, args.base_port)

        if args.dry_run:
            report.print_plan(flows)
            return 0

        containers = runner.get_containers(args.docker, flows)
        runner.run(flows, containers, args.interval)
    except (ScenarioError, FileNotFoundError, docker.errors.DockerException) as e:
        print(f"error: {e}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
