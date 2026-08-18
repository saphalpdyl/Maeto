from .render_certs import cpe_identity, pop_identity

CPE_IDENTITY_MATCH = "*.cpe.maeto.net"


def _addr(value):
    return value.split("/", 1)[0]

def render_cpe_swan_conf(cpe, cpe_plan, attach_pop, attach_access):
    return "\n".join([
        "connections {",
        "  pop {",
        "    version = 2",
        f"    local_addrs = {_addr(cpe_plan.address)}",
        f"    remote_addrs = {_addr(attach_access.address)}",
        "    keyingtries = 0",
        "",
        "    local {",
        "      auth = pubkey",
        "      certs = cert.pem",
        f"      id = {cpe_identity(cpe.id, cpe.customer)}",
        "    }",
        "",
        "    remote {",
        "      auth = pubkey",
        "      cacerts = ca-cert.pem",
        f"      id = {pop_identity(attach_pop.node_name)}",
        "    }",
        "",
        "    children {",
        "      pop {",
        "        mode = tunnel",
        "        local_ts = ::/0",
        "        remote_ts = ::/0",
        "        if_id_in = %unique",
        "        if_id_out = %unique",
        "        start_action = start",
        "      }",
        "    }",
        "  }",
        "}",
        "",
    ])
