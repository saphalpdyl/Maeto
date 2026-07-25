import ipaddress

import yaml

from .constants import (
    DEFAULT_KEYS,
    HOST_KEYS,
    OVERRIDE_KEYS,
    POP_KEYS,
    TOP_LEVEL_KEYS,
)
from .errors import TopologyError
from .model import CoreLink, Defaults, Host, Pop, Topology


def parse_topology(source):
    root = yaml.safe_load(source)
    if not isinstance(root, dict):
        raise TopologyError("topology must be a mapping")
    _reject_unknown(root, TOP_LEVEL_KEYS, "top level")

    name = _require_name(root)
    defaults = _parse_defaults(root)
    pops = _parse_pops(root)
    pop_ids = {p.id for p in pops}
    hosts = _parse_hosts(root, pop_ids)
    links = _parse_links(root, pop_ids)
    return Topology(name, defaults, pops, hosts, links)


def _reject_unknown(d, allowed, where):
    extra = set(d) - allowed
    if extra:
        raise TopologyError(f"unknown key(s) in {where}: {', '.join(sorted(extra))}")


def _require_name(root):
    name = root.get("name")
    if not isinstance(name, str) or not name.strip():
        raise TopologyError("name must be a non-empty string")
    return name


def _parse_defaults(root):
    d = root.get("defaults")
    if not isinstance(d, dict):
        raise TopologyError("defaults must be a mapping")
    _reject_unknown(d, DEFAULT_KEYS, "defaults")
    for key in DEFAULT_KEYS:
        if key not in d:
            raise TopologyError(f"defaults.{key} is required")
    _check_prefix("locator_prefix", d["locator_prefix"], 48)
    _check_prefix("link_prefix", d["link_prefix"], 64)
    _check_prefix("host_prefix", d["host_prefix"], 64)
    return Defaults(d["locator_prefix"], d["link_prefix"], d["host_prefix"])


def _check_prefix(name, value, carve):
    try:
        net = ipaddress.ip_network(value, strict=True)
    except ValueError as e:
        raise TopologyError(f"defaults.{name} is not a valid ipv6 prefix: {e}")
    if net.version != 6:
        raise TopologyError(f"defaults.{name} must be ipv6")
    if net.prefixlen > carve:
        raise TopologyError(f"defaults.{name} must be /{carve} or shorter to carve subnets")


def _parse_pops(root):
    items = root.get("pops")
    if not isinstance(items, list) or not items:
        raise TopologyError("pops must be a non-empty list")
    pops = []
    seen = set()
    for i, raw in enumerate(items):
        if not isinstance(raw, dict):
            raise TopologyError(f"pops[{i}] must be a mapping")
        _reject_unknown(raw, POP_KEYS, f"pops[{i}]")
        pid = _require_id(raw, f"pops[{i}]")
        if pid in seen:
            raise TopologyError(f"duplicate pop id: {pid}")
        seen.add(pid)
        node_name = f"Pop{pid}"
        pops.append(Pop(
            id=pid,
            index=i + 1,
            node_name=node_name,
            clab_label=_clab_label(raw, node_name, f"pops[{i}]"),
            data=_data(raw, f"pops[{i}]"),
        ))
    return pops


def _parse_hosts(root, pop_ids):
    items = root.get("hosts") or []
    if not isinstance(items, list):
        raise TopologyError("hosts must be a list")
    hosts = []
    seen = set()
    for i, raw in enumerate(items):
        if not isinstance(raw, dict):
            raise TopologyError(f"hosts[{i}] must be a mapping")
        _reject_unknown(raw, HOST_KEYS, f"hosts[{i}]")
        hid = _require_id(raw, f"hosts[{i}]")
        if not (hid.startswith("h") and len(hid) > 1):
            raise TopologyError(f"host id must start with 'h' and have a suffix: {hid}")
        if hid in seen:
            raise TopologyError(f"duplicate host id: {hid}")
        seen.add(hid)
        attach = raw.get("attach")
        if not isinstance(attach, str) or attach not in pop_ids:
            raise TopologyError(f"hosts[{i}].attach must reference an existing pop id: {attach}")
        node_name = f"Host{hid[1:]}"
        hosts.append(Host(
            id=hid,
            index=i + 1,
            node_name=node_name,
            clab_label=_clab_label(raw, node_name, f"hosts[{i}]"),
            attach=attach,
            data=_data(raw, f"hosts[{i}]"),
        ))
    return hosts


def _parse_links(root, pop_ids):
    items = root.get("links") or []
    if not isinstance(items, list):
        raise TopologyError("links must be a list")
    links = []
    seen = set()
    index = 0
    for i, raw in enumerate(items):
        if not isinstance(raw, list) or len(raw) not in (2, 3):
            raise TopologyError(f"links[{i}] must be [a, b] or [a, b, count]")
        a, b = raw[0], raw[1]
        for end in (a, b):
            if not isinstance(end, str) or end not in pop_ids:
                raise TopologyError(f"links[{i}] references unknown pop: {end}")
        if a == b:
            raise TopologyError(f"links[{i}] cannot connect a pop to itself: {a}")
        key = frozenset((a, b))
        if key in seen:
            raise TopologyError(f"duplicate link: [{a}, {b}]")
        seen.add(key)
        # expand redundant links into distinct parallel physical links
        for _ in range(_link_count(raw, i)):
            index += 1
            links.append(CoreLink(index=index, a=a, b=b))
    return links


def _link_count(raw, i):
    if len(raw) == 2:
        return 1
    n = raw[2]
    if isinstance(n, bool) or not isinstance(n, int) or n < 1:
        raise TopologyError(f"links[{i}] count must be a positive integer")
    return n


def _require_id(raw, where):
    pid = raw.get("id")
    if not isinstance(pid, str) or not pid:
        raise TopologyError(f"{where}.id must be a non-empty string")
    return pid


def _clab_label(raw, default, where):
    override = raw.get("override")
    if override is None:
        return default
    if not isinstance(override, dict):
        raise TopologyError(f"{where}.override must be a mapping")
    _reject_unknown(override, OVERRIDE_KEYS, f"{where}.override")
    if "clab_label" not in override:
        return default
    label = override["clab_label"]
    if not isinstance(label, str) or not label:
        raise TopologyError(f"{where}.override.clab_label must be a non-empty string")
    return label


def _data(raw, where):
    data = raw.get("data")
    if data is None:
        return {}
    if not isinstance(data, dict):
        raise TopologyError(f"{where}.data must be a mapping")
    return data
