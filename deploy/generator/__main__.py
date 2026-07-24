import argparse
import sys

from .errors import TopologyError
from .parse import parse_topology
from .plan import build_plan
from .state import (
    digest_of,
    is_up_to_date,
    output_dir,
    write_output,
    write_state,
)


def main(argv=None):
    args = _args(argv)

    source = _read_bytes(args.topology)
    digest = digest_of(source)

    if not args.force and is_up_to_date(digest, args.build_dir, args.state_dir):
        print(f"up to date: {output_dir(args.build_dir, digest)}")
        return 0

    try:
        topo = parse_topology(source)
    except TopologyError as e:
        print(f"error: {e}", file=sys.stderr)
        return 1

    plan = build_plan(topo)
    out = write_output(topo, plan, digest, args.build_dir)
    write_state(digest, out, args.state_dir)
    print(f"generated: {out}")
    return 0


def _args(argv):
    p = argparse.ArgumentParser(prog="generator", description="generate a containerlab topology from the dsl")
    p.add_argument("topology", nargs="?", default="deploy/topologies/eight-pop.yaml")
    p.add_argument("--build-dir", default="build")
    p.add_argument("--state-dir", default=".state")
    p.add_argument("--force", action="store_true", help="regenerate even if the hash is unchanged")
    return p.parse_args(argv)


def _read_bytes(path):
    with open(path, "rb") as f:
        return f.read()


if __name__ == "__main__":
    sys.exit(main())
