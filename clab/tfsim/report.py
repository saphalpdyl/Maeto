# Prints the plan, the live status and the end-of-run summary

import time

from typing import List

from iperf import client_cmd, server_cmd
from scenario import Flow


def mbps(bps) -> str:
    if bps is None:
        return "-"
    return f"{bps / 1e6:.1f}"


def print_plan(flows: List[Flow]):
    for flow in flows:
        print(f"{flow.name}  tenant {flow.src.tenant}  {flow.src.attach}->{flow.dst.attach}  start {flow.start:g}s")
        print(f"  {flow.dst.container_name}: {' '.join(server_cmd(flow))}")
        print(f"  {flow.src.container_name}: {' '.join(client_cmd(flow))}")


def print_status(flows: List[Flow], started_at: float):
    elapsed = time.monotonic() - started_at
    print()
    print(f" t={elapsed:5.0f}s")
    print(f" {'flow':<14} {'tenant':>6} {'pops':<6} {'status':<8} {'target':>8} {'sending':>8}")

    for flow in flows:
        pops = f"{flow.src.attach}->{flow.dst.attach}"
        sending = ""
        if flow.status == "running":
            sending = mbps(flow.bps)

        print(f" {flow.name:<14} {flow.src.tenant:>6} {pops:<6} {flow.status:<8} {mbps(flow.rate_bps):>8} {sending:>8}")

    print(flush=True)


def summary_numbers(flow: Flow) -> dict:
    sent = flow.end.get("sum_sent") or flow.end.get("sum") or {}

    recv = {}
    if flow.status != "stopped":
        recv = flow.end.get("sum_received") or {}

    loss = None
    if recv:
        loss = recv.get("lost_percent")

    return {
        "sent": mbps(sent.get("bits_per_second")),
        "recv": mbps(recv.get("bits_per_second")),
        "loss": "-" if loss is None else f"{loss:.2f}",
        "retx": "-" if sent.get("retransmits") is None else str(sent["retransmits"]),
    }


def print_summary(flows: List[Flow]):
    print()
    print(f" {'flow':<14} {'proto':<5} {'target':>8} {'sent':>8} {'recv':>8} {'loss%':>6} {'retx':>6}  status")

    for flow in flows:
        n = summary_numbers(flow)
        status = flow.status
        if flow.error:
            status = f"{status} {flow.error}"

        print(f" {flow.name:<14} {flow.protocol:<5} {mbps(flow.rate_bps):>8} {n['sent']:>8} {n['recv']:>8} "
              f"{n['loss']:>6} {n['retx']:>6}  {status}")
