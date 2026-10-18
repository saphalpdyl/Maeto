import json

from .constants import GENERATOR_VERSION
from .render_certs import cpe_identity


def render_tenant_db(topo):
    # seed data standing in for the control plane's tenant store
    data = {
        "generator_version": GENERATOR_VERSION,
        "classes": [
            {
                "name": tc.name,
                "label": tc.label,
                "index": tc.index,
                "match": {"dscp": list(tc.dscp)},
                "objective": tc.objective,
            }
            for tc in sorted(topo.classes, key=lambda x: x.label)
        ],
        "tiers": [
            {"name": t.name, "index": t.index}
            for t in sorted(topo.tiers.values(), key=lambda x: x.index)
        ],
        "tenants": [],
    }

    # per tunnel, never per tenant -- two sites of one tenant would collide
    next_if_id = 1

    for c in sorted(topo.tenants, key=lambda x: x.id):
        tenant_data = {
            "id": c.id,
            "allocation": c.allocation,
            "tier": c.tier,
            "vrf_table": c.id,
            "sites": [],
        }

        for cp in [x for x in topo.cpes if x.tenant == c.id]:
            tenant_data["sites"].append({
                "cpe": cp.id,
                "portal_id": cp.portal_id,
                "node": cp.node_name,
                "prefix": cp.prefix,
                "attach": cp.attach,
                "attach_node": topo.pop_by_id(cp.attach).node_name,
                "if_id": next_if_id,
                "reservation_mbps": cp.reservation_mbps,
                "classes": cp.class_mbps,
                "identity": cpe_identity(cp.portal_id),
            })
            next_if_id += 1

        data["tenants"].append(tenant_data)

    return json.dumps(data, indent=2) + "\n"
