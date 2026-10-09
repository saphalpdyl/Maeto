# Runs every flow of a scenario at once and reports until they finish

import threading
import time

from typing import Dict, List

import docker

from docker.models.containers import Container

import iperf
import report

from errors import ScenarioError
from scenario import Flow


def get_containers(docker_socket: str, flows: List[Flow]) -> Dict[str, Container]:
    client = docker.DockerClient(base_url=f"unix://{docker_socket}")

    names = set()
    for flow in flows:
        names.add(flow.src.container_name)
        names.add(flow.dst.container_name)

    containers = {}
    for name in sorted(names):
        try:
            containers[name] = client.containers.get(name)
        except docker.errors.NotFound:
            raise ScenarioError(f"container {name} not found; is the lab deployed?")

    return containers


def run(flows: List[Flow], containers: Dict[str, Container], interval: float):
    iperf.kill_all(containers)

    for flow in flows:
        iperf.start_server(flow, containers[flow.dst.container_name])
    time.sleep(1)

    stop = threading.Event()
    started_at = time.monotonic()

    threads = []
    for flow in flows:
        container = containers[flow.src.container_name]
        thread = threading.Thread(target=iperf.run_client, args=(flow, container, stop, started_at), daemon=True)
        thread.start()
        threads.append(thread)

    try:
        while any(thread.is_alive() for thread in threads):
            report.print_status(flows, started_at)
            time.sleep(interval)
    except KeyboardInterrupt:
        print("\ninterrupted; stopping flows, receiver stats are lost for flows still running")
        stop.set()
        iperf.interrupt_clients(containers)
        for thread in threads:
            thread.join(timeout=5)
    finally:
        iperf.kill_all(containers)

    report.print_summary(flows)
