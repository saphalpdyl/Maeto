# static values shared across the generator

GENERATOR_VERSION = "1.3.0"

FRR_IMAGE = "quay.io/frrouting/frr:10.4.1"
FRR_VERSION = "10.5.1"
# both edge roles are plain forwarding linux boxes, no routing daemon
CPE_IMAGE = "maeto-edge:latest"
TRANSIT_IMAGE = "maeto-edge:latest"

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

# only key allowed inside a pop/cpe override block
OVERRIDE_KEYS = {"clab_label"}

TOP_LEVEL_KEYS = {"name", "defaults", "pops", "cpes", "links"}
DEFAULT_KEYS = {"locator_prefix", "link_prefix", "edge_prefix"}
POP_KEYS = {"id", "index", "data", "override"}
CPE_KEYS = {"id", "attach", "data", "override"}

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
