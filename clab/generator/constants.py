# static values shared across the generator

GENERATOR_VERSION = "1.12.0"

POP_IMAGE = "maeto-pop:latest"
FRR_VERSION = "10.5.1"
CPE_IMAGE = "maeto-portal:latest"
# transit models the unmanaged internet: not ours, so stock upstream
TRANSIT_IMAGE = "nicolaka/netshoot:latest"

ISIS_AREA = "49.0000"
ISIS_INSTANCE = "CORE"

# short prefix of the topology digest used for build/<hash>
HASH_DIR_LEN = 12

# bit widths that pack stable addressing indices; a pop index and a redundancy
# instance must each fit in their field, so link/edge ips derive from identity
# (pop index + instance) and never shift when the links list changes
LINK_POP_BITS = 8
LINK_INSTANCE_BITS = 8
CPE_INSTANCE_BITS = 8

# every /64 behind a pop's transit router shares the pop's slice of the edge
# prefix, so one aggregate on the pop covers the uplink and all of its cpes
EDGE_AGGREGATE_PREFIXLEN = 64 - CPE_INSTANCE_BITS
# instance 0 of a pop's edge slice is the pop <-> transit uplink, cpes take 1..n
MAX_CPES_PER_POP = (1 << CPE_INSTANCE_BITS) - 1

# eth1 is reserved on every pop for the link down to its transit router, whether
# or not the pop has cpes; core links are numbered from eth2 up
POP_TRANSIT_IFACE = "eth1"
TRANSIT_UPLINK_IFACE = "eth1"
CPE_IFACE = "eth1"

# the access interface is isolated by a packet filter, not by a routing
# construct. the rendered ruleset is bound in like frr.conf so it is reviewable
# in the build output rather than buried in an exec string.
NFT_FILENAME = "maeto.nft"
NFT_CONTAINER_PATH = "/etc/nftables.d/maeto.nft"

NODE_FILENAME = "node.json"
NODE_CONTAINER_PATH = "/etc/maeto/node.json"

# only key allowed inside a pop/cpe override block
OVERRIDE_KEYS = {"clab_label"}

TOP_LEVEL_KEYS = {"name", "defaults", "pops", "customers", "cpes", "links"}
DEFAULT_KEYS = {"locator_prefix", "link_prefix", "edge_prefix"}
POP_KEYS = {"id", "index", "data", "override"}
CPE_KEYS = {"id", "attach", "customer", "prefix", "data", "override"}
CUSTOMER_KEYS = {"id", "allocation", "data"}

FRR_DAEMONS = """zebra=yes
isisd=yes
staticd=yes

bgpd=no
ospfd=no
ospf6d=no
ripd=no
ripngd=no
pimd=no
pim6d=no
ldpd=no
nhrpd=no
eigrpd=no
babeld=no
sharpd=no
pbrd=no
bfdd=no
fabricd=no
vrrpd=no
pathd=no

vtysh_enable=yes
zebra_options="  -A 127.0.0.1 -s 90000000"
isisd_options="  -A 127.0.0.1"
pathd_options="  -A 127.0.0.1"
staticd_options="-A 127.0.0.1"
"""

# Assumes PYTHONPATH=clab/
CA_KEY_PATH = ".certs/ca-key.pem"
CA_CERT_PATH = ".certs/ca-cert.pem"
CERT_LIFETIME_DAYS = 7300

CA_CERT_BIND = "conf/shared/ca-cert.pem"
CA_CERT_CONTAINER_PATH = "/etc/swanctl/x509ca/ca-cert.pem"
CERT_CONTAINER_PATH = "/etc/swanctl/x509/cert.pem"
KEY_CONTAINER_PATH = "/etc/swanctl/private/key.pem"
