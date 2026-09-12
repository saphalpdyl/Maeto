#!/usr/bin/env python3
# Basic CLI to communicate with control plane's NATS server
import argparse
import asyncio
import json
import os
import sys

import nats
from nats.errors import NoRespondersError, NoServersError
from nats.errors import TimeoutError as NatsTimeoutError

SUBJECT = "maeto.debug.tools.cost_graph.override"

OP_UPSERT = "COST_GRAPH_OVERRIDE_UPSERT"
OP_LIST = "COST_GRAPH_OVERRIDE_LIST"

DIMENSIONS = ("LOSS", "LATENCY", "JITTER")

# containerlab publishes no host port for nats, but it pins the node on the
# management bridge, so the literal address is what works from the host
DEFAULT_SERVER = "nats://[3fff:172:20:20::800:11]:4222"


def main(argv=None):
    args = _args(argv)

    if args.command == "set":
        payload = {
            "operation": OP_UPSERT,
            "edge_id": args.edge_id,
            "cost_dimension": args.dimension,
            "cost": args.cost,
        }
    else:
        payload = {"operation": OP_LIST}
        if args.edge:
            payload["list_filter_edge_ids"] = args.edge
        if args.dim:
            payload["list_filter_cost_dimensions"] = args.dim

    try:
        raw = asyncio.run(_request(args.server, payload, args.timeout))
    except NoServersError as e:
        print(f"error: cannot reach {args.server}: {e}", file=sys.stderr)
        return 1
    except NoRespondersError:
        print(
            f"error: nothing subscribed to {SUBJECT}; the control plane must be "
            "built with -tags devtools (DEBUG=1)",
            file=sys.stderr,
        )
        return 1
    except NatsTimeoutError:
        print(
            f"error: no reply within {args.timeout}s; the control plane rejects "
            "bad requests silently, check its log for the reason",
            file=sys.stderr,
        )
        return 1

    return _render(args.command, raw)


def _render(command, raw):
    try:
        resp = json.loads(raw)
    except ValueError:
        print(f"error: control plane sent a non-json reply: {raw!r}", file=sys.stderr)
        return 1

    if resp.get("error"):
        print(f"error: {resp['error']}", file=sys.stderr)
        return 1

    entries = resp.get("entries") or []
    if not entries:
        print("no matching entries")
        return 0

    if command == "set":
        entry = entries[0]
        previous = entry.get("previous_cost")
        was = "unset" if previous is None else _num(previous)
        print(f"{entry['edge_id']} {entry['cost_dimension']}: {was} -> {_num(entry['cost'])}")
        return 0

    edge_w = max(len(e["edge_id"]) for e in entries)
    dim_w = max(len(e["cost_dimension"]) for e in entries)
    for entry in entries:
        print(
            f"{entry['edge_id']:<{edge_w}}  "
            f"{entry['cost_dimension']:<{dim_w}}  "
            f"{_num(entry['cost'])}"
        )
    return 0


def _num(value):
    return f"{value:g}"


async def _request(server, payload, timeout):
    nc = await nats.connect(
        server,
        connect_timeout=timeout,
        allow_reconnect=False,
        # one attempt, and no default error_cb dumping a traceback per retry
        max_reconnect_attempts=1,
        error_cb=_ignore,
    )
    try:
        reply = await nc.request(SUBJECT, json.dumps(payload).encode(), timeout=timeout)
        return reply.data
    finally:
        await nc.close()


async def _ignore(_):
    pass


def _args(argv):
    p = argparse.ArgumentParser(
        prog="cp-debug",
        description="read and override the control plane cost graph over nats",
        epilog=(
            "examples:\n"
            "  main.py set pop1:eth1-pop2:eth2 LATENCY 250\n"
            "  main.py list --edge pop1:eth1-pop2:eth2 --dim LATENCY\n"
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    p.add_argument(
        "--server",
        default=os.environ.get("NATS_CONNECT_URL", DEFAULT_SERVER),
        help="nats url (default: $NATS_CONNECT_URL or the clab management address)",
    )
    p.add_argument("--timeout", type=float, default=2.0, help="seconds to wait for the reply")

    sub = p.add_subparsers(dest="command", required=True)

    upsert = sub.add_parser("set", help="override one cost dimension on one edge")
    upsert.add_argument("edge_id", help="edge id, e.g. pop1:eth1-pop2:eth2")
    upsert.add_argument("dimension", type=str.upper, choices=DIMENSIONS)
    upsert.add_argument("cost", type=float)

    listing = sub.add_parser("list", help="make the control plane log its cost graph")
    listing.add_argument("--edge", action="append", default=[], metavar="EDGE_ID",
                         help="filter by edge id (repeatable)")
    listing.add_argument("--dim", action="append", default=[], type=str.upper,
                         choices=DIMENSIONS, help="filter by cost dimension (repeatable)")

    return p.parse_args(argv)


if __name__ == "__main__":
    sys.exit(main())
