import json

from .constants import GENERATOR_VERSION
from .render_certs import cpe_identity


def render_tenant_db(topo):
    # seed data standing in for the control plane's tenant store
    data = {
        "generator_version": GENERATOR_VERSION,
        "tenants": [],
    }

    # per tunnel, never per tenant -- two sites of one tenant would collide
    next_if_id = 1

    for c in sorted(topo.tenants, key=lambda x: x.id):
        tenant_data = {
            "id": c.id,
            "allocation": c.allocation,
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
                "identity": cpe_identity(cp.portal_id),
            })
            next_if_id += 1

        data["tenants"].append(tenant_data)

    return json.dumps(data, indent=2) + "\n"
