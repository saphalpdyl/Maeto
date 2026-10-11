## Dataplane
Package `dataplane` contains the dataplane functions and the level-triggered reconciler used by `maeto-agent` and `maeto-portal` to render, plan, diff, and apply the desired state from the controller.

##### Linux Dataplane
Currently, Maeto runs on the Linux networking stack. SRv6 routes and related state are installed with `iproute2` and `vishvananda/netlink`. 

##### VPP Dataplane
VPP as the dataplane is desired, but I don't have a lot of free time. Nevertheless, the dataplane interface is built in a way to support both dataplanes (excluding the nitpicks since I have never worked with VPP).

#### Reconciler
Maeto's level-triggered reconciliation borrows inspiration from Kubernetes' reconciliation mechanism.

The controller sends the full desired state, not a list of changes. The reconciler does not care how the state got there. On every run it:

1. **Renders** the desired state into concrete dataplane objects.
2. **Reads** the current state from the dataplane.
3. **Diffs** the two.
4. **Applies** only the difference.

Because each run works from the latest full state, a missed or failed update fixes itself on the next one. Running it again with the same input changes nothing.
