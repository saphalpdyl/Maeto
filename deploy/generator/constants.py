# static values shared across the generator

GENERATOR_VERSION = "1.1.0"

FRR_IMAGE = "quay.io/frrouting/frr:10.4.1"
FRR_VERSION = "10.5.1"
HOST_IMAGE = "maeto-host:latest"

ISIS_AREA = "49.0000"
ISIS_INSTANCE = "CORE"

# short prefix of the topology digest used for build/<hash>
HASH_DIR_LEN = 12

# bit widths that pack stable addressing indices; a pop index and a redundancy
# instance must each fit in their field, so link/host ips derive from identity
# (pop index + instance) and never shift when the links list changes
LINK_POP_BITS = 8
LINK_INSTANCE_BITS = 8
HOST_INSTANCE_BITS = 8

# only key allowed inside a pop/host override block
OVERRIDE_KEYS = {"clab_label"}

TOP_LEVEL_KEYS = {"name", "defaults", "pops", "hosts", "links"}
DEFAULT_KEYS = {"locator_prefix", "link_prefix", "host_prefix"}
POP_KEYS = {"id", "index", "data", "override"}
HOST_KEYS = {"id", "attach", "data", "override"}

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
