import json

from .constants import GENERATOR_VERSION


def render_customer_db(topo):
    # seed data standing in for the control plane's customer store
    data = {
        "generator_version": GENERATOR_VERSION,
        "customers": [],
    }

    # per tunnel, never per customer -- two sites of one customer would collide
    next_if_id = 1

    for c in sorted(topo.customers, key=lambda x: x.id):
        customer_data = {
            "id": c.id,
            "allocation": c.allocation,
            "vrf_table": c.id,
            "sites": [],
        }

        for cp in [x for x in topo.cpes if x.customer == c.id]:
            customer_data["sites"].append({
                "cpe": cp.id,
                "portal_id": cp.id,
                "node": cp.node_name,
                "prefix": cp.prefix,
                "attach": cp.attach,
                "attach_node": topo.pop_by_id(cp.attach).node_name,
                "if_id": next_if_id,
                "identity": f"cpe-{cp.id}.cust-{c.id}.cpe.maeto.net",
            })
            next_if_id += 1

        data["customers"].append(customer_data)

    return json.dumps(data, indent=2) + "\n"
