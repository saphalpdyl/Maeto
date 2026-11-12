# Prints "<lab> <iface> <iface> ..." for a pop node: its access interface and
# every core interface, which are the ends pop-crash parks and pop-boot returns.
import json
import sys

data = json.load(open(sys.argv[1]))
node = sys.argv[2]

for pop in data["pops"]:
    if pop["name"] != node:
        continue
    ifaces = [i["name"] for i in pop["interfaces"]]
    if pop["access"]:
        ifaces.insert(0, pop["access"]["interface"])
    print(data["name"], *ifaces)
    sys.exit(0)

sys.exit(f"no pop node named {node}")
