# Runs random sessions until stopped or the duration is up, reporting what is on the wire

import random
import threading
import time

from dataclasses import dataclass
from typing import Dict, List

from docker.models.containers import Container

import iperf
import traffic

from report import mbps
from scenario import Flow
from traffic import Config


@dataclass
class AppStats:
    started: int = 0
    done: int = 0
    failed: int = 0
    blocked: int = 0
    bytes: int = 0


class Ports:
    def __init__(self, base: int, count: int):
        self.free = list(range(base, base + count))

    def take(self) -> int:
        return self.free.pop(0)

    def give(self, port: int):
        self.free.append(port)


class Simulation:
    def __init__(self, config: Config, containers: Dict[str, Container], base_port: int):
        self.config = config
        self.containers = containers
        self.rng = random.Random(config.seed)
        self.load = traffic.Load(config, self.rng)
        self.ports = Ports(base_port, config.max_flows)
        self.stats = {app: AppStats() for app in traffic.APPS}
        self.active: List[Flow] = []
        self.threads: List[threading.Thread] = []
        self.lock = threading.Lock()
        self.stop = threading.Event()
        self.started_at = time.monotonic()

    def elapsed(self) -> float:
        return time.monotonic() - self.started_at

    def arrive(self):
        flows = traffic.new_session(self.rng, self.config)
        stats = self.stats[flows[0].app]

        with self.lock:
            # like a full trunk: a session that doesn't fit is turned away, not queued
            if len(self.active) + len(flows) > self.config.max_flows:
                stats.blocked += 1
                return

            stats.started += 1
            for flow in flows:
                flow.port = self.ports.take()
                self.active.append(flow)

        for flow in flows:
            thread = threading.Thread(target=self.run_flow, args=(flow,), daemon=True)
            thread.start()
            self.threads.append(thread)

        self.threads = [thread for thread in self.threads if thread.is_alive()]

    def run_flow(self, flow: Flow):
        src = self.containers[flow.src.container_name]
        dst = self.containers[flow.dst.container_name]

        iperf.start_server(flow, dst)
        if not self.stop.wait(0.5):
            iperf.run_client(flow, src, self.stop, time.monotonic())
        if flow.status != "done":
            iperf.stop_server(flow, dst)

        sent = flow.end.get("sum_sent") or flow.end.get("sum") or {}
        with self.lock:
            stats = self.stats[flow.app]
            stats.bytes += sent.get("bytes") or 0
            if flow.status == "done":
                stats.done += 1
            elif flow.status == "failed":
                stats.failed += 1
            self.active.remove(flow)
            self.ports.give(flow.port)

    def run(self, interval: float):
        iperf.kill_all(self.containers)
        self.started_at = time.monotonic()

        last = self.started_at
        next_report = self.started_at
        next_arrival = self.started_at + self.rng.expovariate(self.load.ceiling())

        try:
            while not self.config.duration or self.elapsed() < self.config.duration:
                now = time.monotonic()
                self.load.drift(now - last)
                last = now

                if now >= next_report:
                    self.print_status()
                    next_report += interval

                # thinning: candidates come at the ceiling rate and each one is
                # kept with the share of it the load wants right now
                if now >= next_arrival:
                    if self.rng.random() < self.load.rate(self.elapsed()) / self.load.ceiling():
                        self.arrive()
                    next_arrival += self.rng.expovariate(self.load.ceiling())

                time.sleep(max(min(next_arrival, next_report) - time.monotonic(), 0))
            print("\nduration reached; stopping flows")
        except KeyboardInterrupt:
            print("\ninterrupted; stopping flows")
        finally:
            self.stop.set()
            iperf.interrupt_clients(self.containers)
            for thread in self.threads:
                thread.join(timeout=5)
            iperf.kill_all(self.containers)

        self.print_status()

    def print_status(self):
        elapsed = self.elapsed()
        hour = self.load.hour(elapsed)
        clock = f"{int(hour):02d}:{int(hour % 1 * 60):02d}"

        with self.lock:
            active = list(self.active)
            rows = {app: AppStats(**vars(stats)) for app, stats in self.stats.items()}

        print()
        print(f" t={elapsed:6.0f}s  day {clock}  load {self.load.rate(elapsed):.2f}/s  "
              f"flows {len(active)}/{self.config.max_flows}")
        print(f" {'app':<11} {'active':>6} {'Mbps':>8} {'started':>8} {'done':>6} {'failed':>6} {'blocked':>7} {'GB':>8}")

        total = 0.0
        for app, stats in rows.items():
            running = [flow for flow in active if flow.app == app]
            bps = sum(flow.bps for flow in running if flow.status == "running")
            total += bps
            print(f" {app:<11} {len(running):>6} {mbps(bps):>8} {stats.started:>8} {stats.done:>6} "
                  f"{stats.failed:>6} {stats.blocked:>7} {stats.bytes / 1e9:>8.2f}")

        print(f" {'total':<11} {len(active):>6} {mbps(total):>8}")
        print(flush=True)


def print_sample(config: Config, count: int):
    rng = random.Random(config.seed)
    for _ in range(count):
        for flow in traffic.new_session(rng, config):
            if flow.size:
                amount = f"{flow.size / 1e6:9.1f} MB"
            else:
                amount = f"{flow.duration:9d} s "

            print(f" {flow.app:<11} tenant {flow.src.tenant:<4} {flow.src.cpe:>4}->{flow.dst.cpe:<4} "
                  f"{flow.protocol}  {mbps(flow.rate_bps):>7} Mbps  {amount}  dscp {flow.dscp}")


def run(config: Config, containers: Dict[str, Container], base_port: int, interval: float):
    Simulation(config, containers, base_port).run(interval)
