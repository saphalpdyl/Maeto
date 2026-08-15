import ipaddress

import yaml

from . import addressing
from .constants import (
    CPE_KEYS,
    DEFAULT_KEYS,
    EDGE_AGGREGATE_PREFIXLEN,
    LINK_INSTANCE_BITS,
    LINK_POP_BITS,
    MAX_CPES_PER_POP,
    OVERRIDE_KEYS,
    POP_KEYS,
    TOP_LEVEL_KEYS,
)
from .errors import TopologyError
from .model import CoreLink, Cpe, Defaults, Pop, Topology


def parse_topology(source):
    root = yaml.safe_load(source)
    if not isinstance(root, dict):
        raise TopologyError("topology must be a mapping")
    _reject_unknown(root, TOP_LEVEL_KEYS, "top level")

    name = _require_name(root)
    defaults = _parse_defaults(root)
    pops = _parse_pops(root)
    pop_index = {p.id: p.index for p in pops}
    cpes = _parse_cpes(root, set(pop_index))
    links = _parse_links(root, pop_index)
    return Topology(name, defaults, pops, cpes, links)


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
    # each pop gets a whole aggregate out of the edge prefix, not a single /64
    _check_prefix("edge_prefix", d["edge_prefix"], EDGE_AGGREGATE_PREFIXLEN)
    return Defaults(d["locator_prefix"], d["link_prefix"], d["edge_prefix"])


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
    seen_ids = set()
    seen_idx = set()
    for i, raw in enumerate(items):
        if not isinstance(raw, dict):
            raise TopologyError(f"pops[{i}] must be a mapping")
        _reject_unknown(raw, POP_KEYS, f"pops[{i}]")
        pid = _require_id(raw, f"pops[{i}]")
        if pid in seen_ids:
            raise TopologyError(f"duplicate pop id: {pid}")
        seen_ids.add(pid)
        index = _pop_index(raw, i, f"pops[{i}]")
        if index in seen_idx:
            raise TopologyError(f"duplicate pop index: {index}")
        seen_idx.add(index)
        node_name = f"Pop{pid}"
        pops.append(Pop(
            id=pid,
            index=index,
            node_name=node_name,
            clab_label=_clab_label(raw, node_name, f"pops[{i}]"),
            data=_data(raw, f"pops[{i}]"),
        ))
    return pops


def _pop_index(raw, i, where):
    # explicit index pins the pop's identity; omitted falls back to declaration order
    if "index" not in raw:
        return i + 1
    n = raw["index"]
    cap = (1 << LINK_POP_BITS) - 1
    if isinstance(n, bool) or not isinstance(n, int) or n < 1:
        raise TopologyError(f"{where}.index must be a positive integer")
    if n > cap:
        raise TopologyError(f"{where}.index must be <= {cap}")
    return n


def _parse_cpes(root, pop_ids):
    items = root.get("cpes") or []
    if not isinstance(items, list):
        raise TopologyError("cpes must be a list")
    cpes = []
    seen = set()
    per_pop = {}
    for i, raw in enumerate(items):
        if not isinstance(raw, dict):
            raise TopologyError(f"cpes[{i}] must be a mapping")
        _reject_unknown(raw, CPE_KEYS, f"cpes[{i}]")
        cid = _require_id(raw, f"cpes[{i}]")
        if not (cid.startswith("c") and len(cid) > 1):
            raise TopologyError(f"cpe id must start with 'c' and have a suffix: {cid}")
        if cid in seen:
            raise TopologyError(f"duplicate cpe id: {cid}")
        seen.add(cid)
        attach = raw.get("attach")
        if not isinstance(attach, str) or attach not in pop_ids:
            raise TopologyError(f"cpes[{i}].attach must reference an existing pop id: {attach}")
        # they all share the pop's transit router, so they all come out of the
        # pop's aggregate
        per_pop[attach] = per_pop.get(attach, 0) + 1
        if per_pop[attach] > MAX_CPES_PER_POP:
            raise TopologyError(f"pop {attach} has more than {MAX_CPES_PER_POP} cpes attached")
        node_name = f"Cpe{cid[1:]}"
        cpes.append(Cpe(
            id=cid,
            node_name=node_name,
            clab_label=_clab_label(raw, node_name, f"cpes[{i}]"),
            attach=attach,
            data=_data(raw, f"cpes[{i}]"),
        ))
    return cpes


def _parse_links(root, pop_index):
    items = root.get("links") or []
    if not isinstance(items, list):
        raise TopologyError("links must be a list")
    links = []
    seen = set()
    for i, raw in enumerate(items):
        if not isinstance(raw, list) or len(raw) not in (2, 3):
            raise TopologyError(f"links[{i}] must be [a, b] or [a, b, count]")
        a, b = raw[0], raw[1]
        for end in (a, b):
            if not isinstance(end, str) or end not in pop_index:
                raise TopologyError(f"links[{i}] references unknown pop: {end}")
        if a == b:
            raise TopologyError(f"links[{i}] cannot connect a pop to itself: {a}")
        key = frozenset((a, b))
        if key in seen:
            raise TopologyError(f"duplicate link: [{a}, {b}]")
        seen.add(key)
        # order endpoints by pop index so subnet + ::1/::2 are position-independent,
        # then expand redundancy into distinct parallel links (each its own stable index)
        lo, hi = sorted((a, b), key=lambda p: pop_index[p])
        for instance in range(1, _link_count(raw, i) + 1):
            idx = addressing.link_subnet_index(pop_index[lo], pop_index[hi], instance)
            links.append(CoreLink(index=idx, a=lo, b=hi, instance=instance))
    return links


def _link_count(raw, i):
    if len(raw) == 2:
        return 1
    n = raw[2]
    if isinstance(n, bool) or not isinstance(n, int) or n < 1:
        raise TopologyError(f"links[{i}] count must be a positive integer")
    if n > (1 << LINK_INSTANCE_BITS):
        raise TopologyError(f"links[{i}] count must be <= {1 << LINK_INSTANCE_BITS}")
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
