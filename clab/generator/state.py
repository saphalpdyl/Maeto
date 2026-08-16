import hashlib
import json
import os
from datetime import datetime, timezone

from .constants import FRR_DAEMONS, GENERATOR_VERSION, HASH_DIR_LEN, NFT_FILENAME, NODE_FILENAME
from .render_clab import render_clab
from .render_data import render_data, render_node
from .render_frr import render_frr
from .render_nft import render_nft
from .render_customer_db import render_customer_db


def digest_of(source):
    return hashlib.sha256(source).hexdigest()


def output_dir(build_dir, digest):
    return os.path.join(build_dir, digest[:HASH_DIR_LEN])


def is_up_to_date(digest, build_dir, state_dir):
    path = os.path.join(state_dir, "latest.json")
    try:
        with open(path) as f:
            state = json.load(f)
    except (OSError, ValueError):
        return False
    return (
        state.get("topology_sha256") == digest
        and state.get("generator_version") == GENERATOR_VERSION
        and os.path.isdir(output_dir(build_dir, digest))
    )


def write_output(topo, plan, digest, build_dir):
    out = output_dir(build_dir, digest)
    _write(os.path.join(out, "topology.yml"), render_clab(topo, plan))
    _write(os.path.join(out, "topology.data.json"), render_data(topo, plan, digest))
    _write(os.path.join(out, "customer.db.json"), render_customer_db(topo))
    _write(os.path.join(out, "conf", "shared", "frr_daemons"), FRR_DAEMONS)
    for pop in topo.pops:
        pp = plan.pops[pop.id]
        _write(os.path.join(out, "conf", pop.node_name, "frr.conf"), render_frr(pp))
        _write(os.path.join(out, "conf", pop.node_name, NODE_FILENAME), render_node(pop, pp))
        # only pops with cpes have an access side to isolate
        if pp.access is not None:
            _write(os.path.join(out, "conf", pop.node_name, NFT_FILENAME), render_nft(pp.access))
    return out


def write_state(digest, out, state_dir):
    state = {
        "topology_sha256": digest,
        "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "output": out,
        "generator_version": GENERATOR_VERSION,
    }
    _write(os.path.join(state_dir, "latest.json"), json.dumps(state, indent=2) + "\n")


def _write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text)
