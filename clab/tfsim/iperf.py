# Starts, reads and stops iperf3 inside the host containers

import json
import threading
import time

from typing import Dict, List

from docker.models.containers import Container

from scenario import Flow


def server_cmd(flow: Flow) -> List[str]:
    return ["iperf3", "-s", "-1", "-p", str(flow.port)]


def client_cmd(flow: Flow) -> List[str]:
    cmd = []

    # a transfer sized in bytes has no end of its own if the path stalls
    if flow.size:
        cmd += ["timeout", str(transfer_deadline(flow))]

    cmd += [
        "iperf3", "-6",
        "-c", flow.dst.address,
        "-p", str(flow.port),
        "-P", str(flow.streams),
        "--json-stream",
        "--connect-timeout", "3000",
    ]

    if flow.size:
        cmd += ["-n", str(flow.size)]
    else:
        cmd += ["-t", str(flow.duration)]

    if flow.rate_bps > 0:
        per_stream = flow.rate_bps // flow.streams
        cmd += ["-b", str(per_stream)]

    if flow.protocol == "udp":
        cmd += ["-u", "-l", str(flow.length)]
    else:
        # hosts have a 9500 MTU and path MTU discovery doesn't make it back
        # through the SRv6 encap, so full-size segments are silently dropped
        cmd += ["-M", str(flow.length)]

    if flow.dscp is not None:
        cmd += ["--dscp", str(flow.dscp)]

    return cmd


def transfer_deadline(flow: Flow) -> int:
    seconds = flow.size * 8 / max(flow.rate_bps, 1)
    return int(seconds * 3) + 10


def start_server(flow: Flow, container: Container):
    container.exec_run(server_cmd(flow), detach=True)


def stop_server(flow: Flow, container: Container):
    container.exec_run(["pkill", "-KILL", "-f", f"^{' '.join(server_cmd(flow))}$"])


def kill_all(containers: Dict[str, Container]):
    for container in containers.values():
        container.exec_run(["pkill", "-KILL", "-x", "iperf3"])


def interrupt_clients(containers: Dict[str, Container]):
    # servers stay up so interrupted clients can still hand over their results
    for container in containers.values():
        container.exec_run(["pkill", "-INT", "-f", "^iperf3 -6 -c "])


def run_client(flow: Flow, container: Container, stop: threading.Event, started_at: float):
    wait = started_at + flow.start - time.monotonic()
    if wait > 0:
        stopped = stop.wait(wait)
        if stopped:
            flow.status = "skipped"
            return

    flow.status = "running"
    _, output = container.exec_run(client_cmd(flow), stream=True)

    pending = b""
    for chunk in output:
        pending += chunk
        while b"\n" in pending:
            line, pending = pending.split(b"\n", 1)
            handle_line(flow, line)
    handle_line(flow, pending)

    flow.bps = 0.0
    finish(flow, stop.is_set())


def finish(flow: Flow, interrupted: bool):
    if interrupted:
        flow.status = "stopped"
        flow.error = ""
        return

    if flow.status != "running":
        return

    sent = flow.end.get("sum_sent") or {}
    if flow.size and flow.end and sent.get("bytes", 0) < flow.size:
        flow.status = "failed"
        flow.error = "transfer timed out"
    elif flow.end:
        flow.status = "done"
    else:
        flow.status = "failed"
        if not flow.error:
            flow.error = "iperf3 exited without results"


def handle_line(flow: Flow, line: bytes):
    line = line.strip()
    if not line:
        return

    try:
        event = json.loads(line)
    except json.JSONDecodeError:
        flow.error = line.decode(errors="replace")
        return

    kind = event.get("event")
    data = event.get("data")

    if kind == "interval":
        flow.bps = data["sum"]["bits_per_second"]
    elif kind == "end":
        flow.end = data
    elif kind == "error":
        flow.status = "failed"
        flow.error = str(data)
