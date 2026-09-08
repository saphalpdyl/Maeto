# Maeto Systems-Centric Dashboard Design Skill

## Purpose

Design and implement the Maeto portal as a **modern network control-plane interface**.

Maeto is an SRv6-based SD-WAN control plane that operates a PoP mesh, consumes telemetry-derived link costs, computes paths, and programs the dataplane.

The UI must therefore behave like an **operational instrument for network engineers**, not like a generic SaaS analytics dashboard.

The primary design objective is:

> Make the current state of the network, Maeto's reasoning, and the resulting dataplane state understandable at a glance and explorable down to individual paths, flows, links, policies, and SIDs.

---

# 1. Core Design Philosophy

## Maeto is a systems interface

Do not design Maeto as:

* a marketing website
* a generic SaaS dashboard
* a CRUD administration panel
* an analytics product
* a collection of KPI cards
* a Grafana clone
* a "cyberpunk hacker" interface

Design it as:

* a network control-plane console
* a network debugger
* an operational topology explorer
* a path-computation observability interface
* a system-state inspection tool

The UI should feel appropriate for an engineer operating a real production network.

Good conceptual references include:

* Cilium Hubble
* ONOS
* Cloudflare Network Analytics
* modern NOC software
* Grafana where appropriate
* IDE/debugger interfaces
* infrastructure control planes

Extract **interaction models**, not visual styles.

Do not copy these products literally.

---

# 2. The Fundamental Mental Model

Maeto's UI should expose this causal chain:

```text
Telemetry
    ↓
Link condition / anomaly
    ↓
Directional link cost
    ↓
PCE path computation
    ↓
Selected path
    ↓
Policy / SRv6 SID list
    ↓
Dataplane programming
    ↓
Observed operational state
```

An operator should be able to traverse this chain interactively.

For any important network state, the UI should answer:

1. **What is happening?**
2. **Why did Maeto make this decision?**
3. **What did Maeto program as a result?**

This is more important than visual decoration.

---

# 3. Primary Objects

Treat the following as first-class domain objects:

* PoPs
* directional links
* physical/logical links
* endpoints
* flows
* paths
* policies
* SRv6 SIDs
* telemetry
* link costs
* anomalies
* dataplane state
* reconciliation state
* controller state
* events

Do not flatten these into generic "resources."

The terminology shown in the UI should correspond to the actual Maeto control-plane model.

---

# 4. Primary Navigation

Prefer navigation organized around **network concepts and operator tasks**.

A reasonable initial structure is:

```text
Overview
Topology
Paths
Flows
Telemetry
Policies
Events
```

Additional sections may be introduced when justified by actual Maeto functionality.

Avoid generic SaaS navigation such as:

```text
Dashboard
Analytics
Reports
Customers
Billing
Settings
```

unless those concepts genuinely exist in the product.

Navigation should answer:

> "What part of the network/control plane am I investigating?"

rather than:

> "Which SaaS page am I opening?"

---

# 5. Topology Is the Primary Workspace

The topology should be treated as a first-class operational surface.

Do not hide the network topology behind a secondary page if it is central to the product.

The topology should support:

* PoP selection
* directional link selection
* zooming and panning
* highlighting
* path visualization
* link-state visualization
* filtering
* contextual inspection
* overlays
* relationship exploration

A user should be able to select:

```text
PoP
 ↓
Link
 ↓
Path
 ↓
Flow
 ↓
Policy
 ↓
SID / dataplane state
```

without losing their place.

---

# 6. Topology Overlays

Use the concept of **semantic overlays**.

The topology graph itself should remain stable while the displayed meaning can change.

Potential Maeto overlays:

```text
Topology
Traffic
Costs
Health
Paths
Telemetry
SRv6
Reconciliation
```

Examples:

### Cost overlay

Links communicate current directional cost.

### Health overlay

Links communicate operational health/anomaly state.

### Traffic overlay

Links communicate traffic volume/utilization.

### Path overlay

A selected computed path is emphasized across the mesh.

### SRv6 overlay

Relevant SIDs and programmed segments become inspectable.

### Reconciliation overlay

The topology communicates desired vs actual dataplane state.

Do not simultaneously display every possible metric.

The goal is **semantic focus**.

---

# 7. Directionality Matters

Maeto models links directionally.

Therefore:

```text
A → B
```

and:

```text
B → A
```

must be treated as potentially different objects.

Do not visually imply that a bidirectional relationship necessarily has identical state.

Directional differences may exist in:

* cost
* telemetry
* utilization
* capacity
* anomaly state
* path selection
* failures

The UI must make direction obvious.

---

# 8. Link Inspection

Selecting a link should expose a contextual detail surface.

Example information:

```text
A → B

Health              Healthy
Cost                0.37
Anomaly score       0.11
Capacity            10 Gbps
Utilization         42%

Active paths        183
Reroutes (24h)      7
```

Then expose the causal information:

```text
Cost history
Telemetry
Affected paths
Affected flows
```

The operator should be able to understand **why this link currently has its cost**.

Do not expose raw telemetry without contextualizing how Maeto uses it.

---

# 9. Path Inspection

A path is a first-class object.

Represent it as an ordered sequence of network elements:

```text
POP-A
  ↓
POP-C
  ↓
POP-D
  ↓
EGRESS
```

Path inspection should expose:

```text
Path

A → C → D → Egress

Policy             latency-sensitive
Cost               1.82
Selection reason   lowest current cost
State              programmed
Flows              14,203
```

Where available, expose:

* constituent links
* total cost
* constraints
* policy
* computation timestamp
* current validity
* affected flows
* SID list
* programmed state
* reason for selection

---

# 10. Decision Explainability

Maeto is a control plane.

The UI must not merely show **what path was selected**.

It should expose **why**.

For example:

```text
Selected path

A → C → D

Reason:
Lowest-cost path satisfying
latency constraint.

Compared with:

A → B → D
Cost: 2.41

A → C → D
Cost: 1.82  ← selected
```

This should be implemented wherever meaningful.

Avoid opaque:

```text
Status: Active
```

when Maeto can explain the underlying decision.

---

# 11. Flow Inspection

Flows should connect the logical policy layer to the actual selected path.

A flow inspection surface may expose:

```text
Flow

Source       ...
Destination  ...
Protocol     ...
Ports        ...

Policy       low-latency
Path         A → C → D

SR Policy
SID 1
SID 2
SID 3

Dataplane
Programmed ✓
```

The operator should be able to move from:

```text
Flow → Path → Links → Telemetry
```

and:

```text
Flow → Policy → SRv6 → Dataplane
```

---

# 12. SRv6 Should Be Inspectable, Not Decorative

SRv6 is fundamental to Maeto.

Where appropriate, expose:

* SR policies
* SID lists
* endpoint behavior
* segment ordering
* locator information
* programmed state
* dataplane reconciliation

Do not expose implementation details everywhere.

SRv6 information should appear when the operator is investigating the path/programming layer.

The UI should answer:

> "What did Maeto actually program?"

---

# 13. Desired vs Actual State

Maeto is a controller and therefore has a distinction between:

```text
Desired state
```

and:

```text
Actual dataplane state
```

Where applicable, expose this explicitly.

For example:

```text
Policy
Desired      A → C → D
Actual       A → C → D
State        Reconciled ✓
```

or:

```text
Policy
Desired      A → C → D
Actual       A → B → D
State        Drift detected
```

Reconciliation failures should be operationally obvious without dominating the entire UI.

---

# 14. Events and State Changes

Provide an event-oriented view where useful.

Events may include:

* link degradation
* anomaly detection
* cost changes
* path recomputation
* policy changes
* dataplane programming
* reconciliation
* failures
* recovery

Events should connect to affected objects.

For example:

```text
23:41:02  Link A → B degraded
23:41:03  Cost changed 0.42 → 1.71
23:41:03  37 paths recomputed
23:41:04  12 policies updated
23:41:04  Dataplane reconciliation complete
```

The operator should be able to click through to the relevant objects.

---

# 15. Density

Maeto is intended for technical users.

Do not automatically maximize whitespace.

Prefer:

* compact tables
* information-dense side panels
* inline metadata
* compact status indicators
* contextual drill-down
* expandable rows
* keyboard navigation where useful

However:

> Dense does not mean cluttered.

Use hierarchy, alignment, grouping and typography to make dense information scannable.

Every displayed element should justify its existence.

---

# 16. Avoid Dashboard Card Spam

Do not default to:

```text
┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐
│ 183    │ │ 42     │ │ 7      │ │ 99.9%  │
│ Paths  │ │ Links  │ │ Alerts │ │ Uptime │
└────────┘ └────────┘ └────────┘ └────────┘
```

Cards may be appropriate for a small number of high-value summaries, but they should not become the fundamental UI primitive.

Prefer actual operational objects.

For example:

```text
LINKS

A → B     10G    42%    cost 0.37    healthy
A → C     10G    71%    cost 0.81    healthy
B → D     40G    88%    cost 1.92    degraded
```

This communicates more operationally useful information.

---

# 17. Tables

Tables are encouraged when they represent real operational collections.

Good table characteristics:

* compact rows
* clear alignment
* sortable columns
* filtering
* search
* meaningful status indicators
* contextual actions
* row expansion
* drill-down

Do not turn every table into a giant spreadsheet.

Show the fields needed for the operator's current question.

---

# 18. Search and Filtering

Maeto should eventually support fast discovery of objects.

Useful search targets include:

* PoPs
* links
* flows
* endpoints
* policies
* SIDs
* paths

Filtering should be contextual.

For example:

```text
Topology
[ Health: Degraded ]
[ Direction: A → B ]
[ Cost: > 1.0 ]
```

or:

```text
Paths
[ Policy: Low Latency ]
[ Status: Recomputed ]
```

Avoid forcing users through multiple pages to narrow down operational state.

---

# 19. Color Semantics

Color should communicate **meaning**, not decoration.

Reserve strong colors for things such as:

* healthy
* degraded
* failed
* selected
* warning
* active
* drift

Do not use excessive colors merely to make the interface "interesting."

A mostly restrained interface with carefully chosen semantic highlights is preferable.

---

# 20. Dark-First, Not Cyberpunk

A dark interface is appropriate for Maeto, but avoid:

* neon overload
* glowing borders
* excessive gradients
* fake terminal aesthetics
* gratuitous green text
* sci-fi HUD elements

The target is:

> modern infrastructure software

not:

> spaceship control panel

Typography, spacing and hierarchy should carry the design.

---

# 21. Motion

Motion should communicate state transitions.

Good uses:

* topology changes
* path selection
* panel transitions
* live state changes
* loading/reconciliation
* event highlighting

Avoid decorative animations that compete with operational information.

An operator should never have to wait for an animation to finish to understand state.

---

# 22. Responsive Behavior

The desktop experience is the primary target because Maeto is an engineering operations interface.

Nevertheless:

* panels should resize sensibly
* topology should remain usable
* tables should degrade gracefully
* critical state should remain accessible
* controls should not overlap

Do not design primarily for mobile unless the product requirements explicitly change.

---

# 23. Interaction Pattern

Prefer a persistent workspace with contextual inspection.

A strong default pattern is:

```text
┌─────────────┬──────────────────────────────┬───────────────┐
│ Navigation  │                              │               │
│             │          Topology            │   Inspector   │
│             │                              │               │
│             │                              │               │
│             │                              │               │
└─────────────┴──────────────────────────────┴───────────────┘
```

Selecting an object should populate the inspector without unnecessarily navigating away.

This preserves spatial context.

---

# 24. Information Hierarchy

Prioritize information approximately as follows:

### Level 1 — Operational state

What is happening right now?

### Level 2 — Network relationships

Which PoP/link/path/flow is involved?

### Level 3 — Decision

Why did Maeto choose this?

### Level 4 — Implementation

What policy/SID/dataplane state resulted?

### Level 5 — Historical context

How did this state evolve?

Do not put historical analytics above current operational state unless the user explicitly asks for historical analysis.

---

# 25. Loading and Failure States

Treat loading and failure as first-class system states.

Do not use generic:

```text
Loading...
```

when more useful information is available.

Prefer:

```text
Loading topology
Fetching 42 PoPs...
```

or:

```text
Reconciliation pending
12 policies awaiting dataplane confirmation
```

Errors should identify:

* what failed
* affected object
* current state
* whether Maeto will retry
* what the operator can inspect next

---

# 26. Real-Time State

Maeto represents a changing network.

The UI should make real-time state distinguishable from stale information.

Where appropriate, show:

* last update time
* live/reconnecting state
* event stream
* state transition
* pending reconciliation

Do not pretend that static data is live.

---

# 27. Implementation Guidance

When implementing the frontend:

1. Establish the domain model first.
2. Define reusable design tokens.
3. Build the topology workspace.
4. Build contextual inspectors.
5. Build tables and filtering.
6. Build path/flow drill-down.
7. Add event/state visualization.
8. Add visual polish.
9. Perform browser-based visual QA.
10. Iterate based on actual rendered output.

Do not start by creating dozens of generic UI components.

Start with the **operator workflows**.

---

# 28. Component Philosophy

Components should correspond to meaningful concepts.

Prefer:

```text
PopTopology
LinkInspector
PathInspector
FlowInspector
PolicyInspector
TelemetryPanel
ReconciliationState
EventTimeline
```

over excessively generic abstractions such as:

```text
GenericCard
GenericPanel
GenericThing
DashboardWidget
```

Generic primitives are fine underneath, but the domain layer should remain explicit.

---

# 29. Visual References

Use these projects as conceptual references:

### Cilium Hubble

Use for:

* topology/graph interaction
* flow exploration
* filtering
* object drill-down
* relationship-oriented visualization

### ONOS

Use for:

* SDN topology interaction
* topology overlays
* contextual inspection
* controller-oriented workflows

### Cloudflare Network Analytics

Use for:

* information hierarchy
* scoped filtering
* network analytics
* high-density operational information

### NOC

Use for:

* network-management information architecture
* inventory/topology concepts
* operational workflows

### Modern IDEs/debuggers

Use for:

* persistent workspace
* contextual inspection
* hierarchical drill-down
* state/debug information

**Do not copy their branding or aesthetics.**

---

# 30. Design Anti-Patterns

Avoid:

* generic SaaS dashboards
* giant hero metrics
* excessive rounded cards
* excessive whitespace
* purple gradients
* glassmorphism
* decorative backgrounds
* meaningless charts
* fake real-time animations
* excessive neon
* terminal cosplay
* arbitrary color usage
* hiding topology behind analytics pages
* requiring navigation away from topology to inspect objects
* presenting decisions without explanations
* showing telemetry without connecting it to Maeto's decisions
* showing SRv6 configuration without connecting it to the selected path
* showing dataplane state without desired-vs-actual context

---

# 31. The Golden Rule

Whenever adding a UI element, ask:

> **Does this help an operator understand or operate the network?**

If the answer is no, remove it.

The Maeto portal should make the control plane **legible**.

The ultimate interaction should feel like:

```text
"I see something wrong."
        ↓
"I select it."
        ↓
"I understand what Maeto observed."
        ↓
"I understand the cost/decision."
        ↓
"I see the resulting path."
        ↓
"I see the affected flows."
        ↓
"I see what Maeto programmed."
        ↓
"I can determine whether the dataplane converged."
```

That causal chain is the central organizing principle of the Maeto UI.
