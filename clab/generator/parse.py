import ipaddress

import yaml

from . import addressing
from .constants import (
    CLASS_KEYS,
    CPE_KEYS,
    DSCP_MAX,
    DSCP_UNMARKED,
    MATCH_KEYS,
    DEFAULT_CLASS_LABEL,
    DEFAULT_CLASS_NAME,
    OBJECTIVE_DIMENSIONS,
    TIER_KEYS,
    TENANT_KEYS,
    DEFAULT_KEYS,
    DEFAULT_START_JITTER,
    DEFAULT_TENANT_TIER,
    LINK_KEYS,
    LINK_PARALLEL_KEYS,
    EDGE_AGGREGATE_PREFIXLEN,
    LINK_INSTANCE_BITS,
    LINK_POP_BITS,
    MAX_CPES_PER_POP,
    OVERRIDE_KEYS,
    POP_KEYS,
    TOP_LEVEL_KEYS,
)
from .errors import TopologyError
from .model import CoreLink, Cpe, Tenant, Defaults, Pop, Tier, Topology, TrafficClass


def parse_topology(source):
    root = yaml.safe_load(source)
    if not isinstance(root, dict):
        raise TopologyError("topology must be a mapping")
    _reject_unknown(root, TOP_LEVEL_KEYS, "top level")

    name = _require_name(root)
    defaults = _parse_defaults(root)
    pops = _parse_pops(root)
    pop_index = {p.id: p.index for p in pops}
    classes = _parse_classes(root)
    tiers = _parse_tiers(root, {c.name for c in classes})
    tenants = _parse_tenants(root)
    cpes = _parse_cpes(root, set(pop_index), tenants, tiers, {c.name for c in classes})
    links = _parse_links(root, pop_index)
    return Topology(name, defaults, pops, tenants, cpes, links, classes, tiers)


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


def _parse_classes(root):
    items = root.get("classes") or {}
    if not isinstance(items, dict):
        raise TopologyError("classes must be a mapping of class name to definition")
    classes = []
    seen_labels = {}
    seen_indexes = {}
    seen_dscp = {}
    for name, raw in items.items():
        where = f"classes.{name}"
        if not isinstance(name, str) or not name.strip():
            raise TopologyError("class names must be non-empty strings")
        if name == DEFAULT_CLASS_NAME:
            raise TopologyError(f"{where}: '{DEFAULT_CLASS_NAME}' is implicit and cannot be declared")
        if not isinstance(raw, dict):
            raise TopologyError(f"{where} must be a mapping")
        _reject_unknown(raw, CLASS_KEYS, where)
        label = raw.get("label")
        if isinstance(label, bool) or not isinstance(label, int) or label < 0:
            raise TopologyError(f"{where}.label must be a non-negative integer")
        if label == DEFAULT_CLASS_LABEL:
            raise TopologyError(f"{where}.label {DEFAULT_CLASS_LABEL} is reserved for the default class")
        if label in seen_labels:
            raise TopologyError(f"{where}.label {label} is already used by class {seen_labels[label]}")
        seen_labels[label] = name
        index = _priority_index(raw, seen_indexes, name, where, 1)
        dscp = _match_dscp(raw.get("match"), seen_dscp, name, f"{where}.match")
        classes.append(TrafficClass(
            name=name,
            label=label,
            index=index,
            dscp=dscp,
            objective=_objective(raw.get("objective"), f"{where}.objective"),
        ))
    return classes


def _priority_index(raw, seen, name, where, minimum):
    index = raw.get("index")
    if isinstance(index, bool) or not isinstance(index, int) or index < minimum:
        raise TopologyError(f"{where}.index must be an integer >= {minimum}")
    if index in seen:
        raise TopologyError(f"{where}.index {index} is already used by {seen[index]}")
    seen[index] = name
    return index


def _match_dscp(raw, seen, name, where):
    if not isinstance(raw, dict):
        raise TopologyError(f"{where} must be a mapping")
    _reject_unknown(raw, MATCH_KEYS, where)
    codepoints = raw.get("dscp")
    if not isinstance(codepoints, list) or not codepoints:
        raise TopologyError(f"{where}.dscp must be a non-empty list of codepoints")
    out = []
    for value in codepoints:
        if isinstance(value, bool) or not isinstance(value, int) or not 0 <= value <= DSCP_MAX:
            raise TopologyError(f"{where}.dscp values must be integers in 0..{DSCP_MAX}: {value}")
        if value == DSCP_UNMARKED:
            raise TopologyError(f"{where}.dscp {DSCP_UNMARKED} is unmarked traffic and always rides the default class")
        if value in out:
            raise TopologyError(f"{where}.dscp {value} is listed twice")
        if value in seen:
            raise TopologyError(f"{where}.dscp {value} is already matched by class {seen[value]}")
        seen[value] = name
        out.append(value)
    return tuple(sorted(out))


def _objective(raw, where):
    if not isinstance(raw, dict) or not raw:
        raise TopologyError(f"{where} must be a non-empty mapping of dimension to weight")
    _reject_unknown(raw, OBJECTIVE_DIMENSIONS, where)
    out = {}
    for dim, weight in raw.items():
        if isinstance(weight, bool) or not isinstance(weight, (int, float)) or weight <= 0:
            raise TopologyError(f"{where}.{dim} must be a number greater than zero")
        out[dim] = float(weight)
    return out


def _parse_tiers(root, class_names):
    items = root.get("tiers") or {}
    if not isinstance(items, dict):
        raise TopologyError("tiers must be a mapping of tier name to definition")
    tiers = {}
    seen_indexes = {}
    for name, raw in items.items():
        where = f"tiers.{name}"
        if not isinstance(name, str) or not name.strip():
            raise TopologyError("tier names must be non-empty strings")
        if not isinstance(raw, dict):
            raise TopologyError(f"{where} must be a mapping")
        _reject_unknown(raw, TIER_KEYS, where)
        shares = _class_split(raw.get("classes"), class_names, f"{where}.classes", _fraction)
        if sum(shares.values()) > 1:
            raise TopologyError(f"{where}.classes add up to more than 1")
        index = _priority_index(raw, seen_indexes, name, where, 1)
        tiers[name] = Tier(name=name, index=index, shares=shares)
    return tiers


def _class_split(raw, class_names, where, value_of):
    if raw is None:
        return {}
    if not isinstance(raw, dict):
        raise TopologyError(f"{where} must be a mapping of class name to amount")
    out = {}
    for name, value in raw.items():
        if name not in class_names:
            raise TopologyError(f"{where}.{name} does not reference a declared class")
        out[name] = value_of(value, f"{where}.{name}")
    return out


def _fraction(value, where):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not 0 < value <= 1:
        raise TopologyError(f"{where} must be a fraction in (0, 1]")
    return float(value)


def _site_class_mbps(raw, reservation, tier, tiers, class_names, where):
    # an explicit split replaces the tier's template outright rather than merging
    if "classes" in raw:
        split = _class_split(raw["classes"], class_names, f"{where}.classes", _mbps)
        if sum(split.values()) > reservation:
            raise TopologyError(f"{where}.classes add up to more than reservation_bandwidth {reservation}")
        return split
    template = tiers.get(tier)
    if template is None:
        return {}
    return {name: round(share * reservation, 6) for name, share in template.shares.items()}


def _parse_tenants(root):
    items = root.get("tenants") or []
    if not isinstance(items, list):
        raise TopologyError("tenants must be a list")
    tenants = []
    seen = set()
    for i, raw in enumerate(items):
        if not isinstance(raw, dict):
            raise TopologyError(f"tenants[{i}] must be a mapping")
        _reject_unknown(raw, TENANT_KEYS, f"tenants[{i}]")
        cid = raw.get("id")
        if isinstance(cid, bool) or not isinstance(cid, int) or cid < 1:
            raise TopologyError(f"tenants[{i}].id must be a positive integer")
        if cid in seen:
            raise TopologyError(f"duplicate tenant id: {cid}")
        seen.add(cid)
        alloc = _require_network(raw.get("allocation"), f"tenants[{i}].allocation")
        tier = _tenant_tier(raw, f"tenants[{i}]")
        tenants.append(Tenant(
            id=cid,
            allocation=str(alloc),
            tier=tier,
            data=_data(raw, f"tenants[{i}]"),
        ))
    return tenants


def _tenant_tier(raw, where):
    tier = raw.get("tier", DEFAULT_TENANT_TIER)
    if not isinstance(tier, str) or tier.strip() == "":
        raise TopologyError(f"{where}.tier must be a non-empty string")
    return tier.strip()


def _require_network(value, where):
    if not isinstance(value, str):
        raise TopologyError(f"{where} must be a string")
    try:
        net = ipaddress.ip_network(value, strict=True)
    except ValueError as e:
        raise TopologyError(f"{where} is not a valid network: {e}")
    if net.version != 6:
        raise TopologyError(f"{where} must be ipv6")
    return net


def _parse_cpes(root, pop_ids, tenants, tiers, class_names):
    items = root.get("cpes") or []
    if not isinstance(items, list):
        raise TopologyError("cpes must be a list")
    by_id = {c.id: c for c in tenants}
    cpes = []
    seen = set()
    per_pop = {}
    per_tenant = {}
    seen_portal_ids = []
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
        cust = raw.get("tenant")
        if cust not in by_id:
            raise TopologyError(f"cpes[{i}].tenant must reference an existing tenant id: {cust}")
        prefix = _require_network(raw.get("prefix"), f"cpes[{i}].prefix")
        alloc = ipaddress.ip_network(by_id[cust].allocation)
        if not prefix.subnet_of(alloc):
            raise TopologyError(f"cpes[{i}].prefix {prefix} is outside tenant {cust} allocation {alloc}")
        # sites of one tenant share a vrf, so overlapping prefixes break routing
        for other in per_tenant.setdefault(cust, []):
            if prefix.overlaps(other):
                raise TopologyError(f"cpes[{i}].prefix {prefix} overlaps {other} on tenant {cust}")
        per_tenant[cust].append(prefix)
        node_name = f"Cpe{cid[1:]}"

        portal_id = raw.get("portal_id", "")
        if portal_id == "":
            raise TopologyError(f"{node_name} is missing portal ID")

        if portal_id in seen_portal_ids:
            raise TopologyError(f"{portal_id} is a duplicate. Portal IDs must be unique")

        if len(portal_id) != 12:
            raise TopologyError(f"portal_id must be 12 character long")

        seen_portal_ids.append(portal_id)

        reservation = _mbps(raw.get("reservation_bandwidth"), f"cpes[{i}].reservation_bandwidth")
        class_mbps = _site_class_mbps(raw, reservation, by_id[cust].tier, tiers, class_names, f"cpes[{i}]")

        cpes.append(Cpe(
            id=cid,
            tenant=cust,
            prefix=str(prefix),
            node_name=node_name,
            clab_label=_clab_label(raw, node_name, f"cpes[{i}]"),
            attach=attach,
            data=_data(raw, f"cpes[{i}]"),
            portal_id=portal_id,
            reservation_mbps=reservation,
            class_mbps=class_mbps,
        ))
    return cpes


def _parse_links(root, pop_index):
    items = root.get("links") or []
    if not isinstance(items, list):
        raise TopologyError("links must be a list")
    links = []
    seen = set()
    for i, raw in enumerate(items):
        if not isinstance(raw, dict):
            raise TopologyError(
                f"links[{i}] must be a mapping with a, b and parallel"
            )
        _reject_unknown(raw, LINK_KEYS, f"links[{i}]")
        a, b = raw.get("a"), raw.get("b")
        for end in (a, b):
            if not isinstance(end, str) or end not in pop_index:
                raise TopologyError(f"links[{i}] references unknown pop: {end}")
        if a == b:
            raise TopologyError(f"links[{i}] cannot connect a pop to itself: {a}")
        key = frozenset((a, b))
        if key in seen:
            raise TopologyError(f"duplicate link: [{a}, {b}]")
        seen.add(key)

        specs = _link_parallel(raw, i)

        # order endpoints by pop index so subnet + ::1/::2 are position-independent,
        # then expand redundancy into distinct parallel links (each its own stable
        # index). each parallel link carries its own capacity and delay, so two
        # links between the same pair are not interchangeable
        lo, hi = sorted((a, b), key=lambda p: pop_index[p])
        for instance, spec in enumerate(specs, start=1):
            idx = addressing.link_subnet_index(pop_index[lo], pop_index[hi], instance)
            links.append(CoreLink(
                index=idx,
                a=lo,
                b=hi,
                instance=instance,
                bandwidth_mbps=spec["bandwidth"],
                delay_ms=spec["delay"],
                start_jitter=spec["start_jitter"],
            ))
    return links


def _link_parallel(raw, i):
    specs = raw.get("parallel")
    if not isinstance(specs, list) or len(specs) < 1:
        raise TopologyError(f"links[{i}].parallel must be a non-empty list")
    if len(specs) > (1 << LINK_INSTANCE_BITS):
        raise TopologyError(
            f"links[{i}].parallel must hold <= {1 << LINK_INSTANCE_BITS} links"
        )

    out = []
    for j, spec in enumerate(specs):
        where = f"links[{i}].parallel[{j}]"
        if not isinstance(spec, dict):
            raise TopologyError(f"{where} must be a mapping")
        _reject_unknown(spec, LINK_PARALLEL_KEYS, where)
        out.append({
            "bandwidth": _mbps(spec.get("bandwidth"), f"{where}.bandwidth"),
            "delay": _delay_ms(spec.get("delay"), f"{where}.delay"),
            "start_jitter": _start_jitter(spec.get("start_jitter"), f"{where}.start_jitter"),
        })
    return out


def _mbps(value, where):
    # mbps throughout, no unit suffixes to parse
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise TopologyError(f"{where} must be a number in Mbps")
    if value <= 0:
        raise TopologyError(f"{where} must be greater than zero")
    return float(value)


def _delay_ms(value, where):
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise TopologyError(f"{where} must be a number in milliseconds")
    if value < 0:
        raise TopologyError(f"{where} must not be negative")
    return float(value)


def _start_jitter(value, where):
    if value is None:
        return DEFAULT_START_JITTER
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise TopologyError(f"{where} must be a number between 0 and 1")
    if not 0 <= value <= 1:
        raise TopologyError(f"{where} must be between 0 and 1")
    return float(value)


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
