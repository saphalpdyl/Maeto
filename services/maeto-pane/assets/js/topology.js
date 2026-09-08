import cytoscape from "../vendor/cytoscape.min.js"
// fcose reaches for cose-base/layout-base by bare name; config/config.exs points
// esbuild at the vendored copies
import fcose from "../vendor/cytoscape-fcose.js"

cytoscape.use(fcose)

// The topology is the one surface LiveView does not own: cytoscape keeps its own
// canvas, so the element carries phx-update="ignore" and every change arrives as
// a JSON payload on data-graph. LiveView merges data-* attributes even on an
// ignored element and calls updated() when the dataset changes.
//
// A pop is a box, not a blob: the cytoscape node draws the frame so edges clip
// to it and selection styling works, and an html overlay carries the typography.
//
// Layout is expensive, so it only re-runs when the node/edge set actually
// changes. Selection and overlay changes are pure restyles.

const NODE_W = 172
const NODE_H = 58

const token = name => {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name)
  return value.trim() || "#888888"
}

const palette = () => ({
  canvas: token("--mt-canvas"),
  surface: token("--mt-surface"),
  sunken: token("--mt-sunken"),
  ink: token("--mt-ink"),
  muted: token("--mt-muted"),
  faint: token("--mt-faint"),
  line: token("--mt-line"),
  lineStrong: token("--mt-line-strong"),
  accent: token("--mt-accent"),
  ok: token("--mt-ok"),
  warn: token("--mt-warn"),
  bad: token("--mt-bad"),
  idle: token("--mt-idle"),
})

const statusColor = (colors, status) =>
  ({
    ok: colors.ok,
    converging: colors.warn,
    issue: colors.bad,
    blind: colors.idle,
    silent: colors.faint,
  }[status] || colors.faint)

// cost is a magnitude, so it steps one hue rather than cycling status colours
const costStep = ratio => {
  if (ratio > 0.75) return {width: 4, opacity: 1}
  if (ratio > 0.5) return {width: 3, opacity: 0.8}
  if (ratio > 0.25) return {width: 2.5, opacity: 0.62}
  return {width: 2, opacity: 0.45}
}

const signature = graph =>
  JSON.stringify([graph.nodes.map(n => n.id).sort(), graph.edges.map(e => e.id).sort()])

const escape = value =>
  String(value == null ? "" : value).replace(
    /[&<>"']/g,
    ch => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"})[ch],
  )

const clip = (value, max) => {
  const text = String(value == null ? "" : value)
  return text.length > max ? `${text.slice(0, max - 1)}…` : text
}

// The card is drawn as the node's own background, so it can carry real
// typography while staying pixel-aligned with the box the edges clip to. An
// html overlay drifts out of step with layout and pan; this cannot.
const card = (colors, data) => {
  const facts = [
    data.sites === 1 ? "1 site" : `${data.sites} sites`,
    data.tenants === 1 ? "1 tenant" : `${data.tenants} tenants`,
  ].join(" · ")

  const mono = "ui-monospace, SFMono-Regular, Menlo, monospace"
  const sans = "ui-sans-serif, system-ui, -apple-system, sans-serif"

  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${NODE_W}" height="${NODE_H}">
    <circle cx="13" cy="15" r="3.5" fill="${statusColor(colors, data.status)}"/>
    <text x="23" y="19" font-family="${mono}" font-size="12" font-weight="600" fill="${colors.ink}">${escape(clip(data.id, 4))}</text>
    <text x="${23 + 9 * String(clip(data.id, 4)).length + 6}" y="19" font-family="${sans}" font-size="11" fill="${colors.muted}">${escape(clip(data.name, 12))}</text>
    <text x="13" y="34" font-family="${mono}" font-size="10" fill="${colors.faint}">${escape(clip(data.locator || "no locator", 22))}</text>
    <text x="13" y="47" font-family="${sans}" font-size="10" fill="${colors.muted}">${escape(facts)}</text>
  </svg>`

  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`
}

const stylesheet = colors => [
  {
    selector: "node[kind = 'pop']",
    style: {
      shape: "round-rectangle",
      width: NODE_W,
      height: NODE_H,
      "background-color": colors.surface,
      "background-image": node => card(colors, node.data()),
      "background-fit": "none",
      "background-width": NODE_W,
      "background-height": NODE_H,
      "background-position-x": 0,
      "background-position-y": 0,
      "background-clip": "node",
      "border-color": colors.line,
      "border-width": 1,
      label: "",
      "transition-property": "border-color, border-width, opacity",
      "transition-duration": "120ms",
    },
  },
  {
    selector: "node.traced",
    style: {"border-color": colors.accent, "border-width": 2},
  },
  {
    selector: "node.picked",
    style: {"border-color": colors.accent, "border-width": 2, "background-color": colors.sunken},
  },
  {
    selector: "node.dimmed",
    style: {opacity: 0.32},
  },
  {
    selector: "edge",
    style: {
      "curve-style": "bezier",
      "control-point-step-size": 34,
      width: 2,
      "line-color": colors.lineStrong,
      "target-arrow-shape": "none",
      opacity: 1,
      "transition-property": "line-color, width, opacity",
      "transition-duration": "120ms",
    },
  },
  {
    selector: "edge[!up]",
    style: {"line-color": colors.bad, "line-style": "dashed", "line-dash-pattern": [6, 4]},
  },
  {
    selector: "edge.traced",
    style: {"line-color": colors.accent, width: 3.5, opacity: 1},
  },
  {
    selector: "edge.picked",
    style: {"line-color": colors.accent, width: 3.5},
  },
  {
    selector: "edge.dimmed",
    style: {opacity: 0.15},
  },
  {
    selector: "edge.labelled",
    style: {
      label: "data(cost)",
      "font-size": 10,
      "font-family": "var(--font-mono, monospace)",
      color: colors.accent,
      "text-background-color": colors.canvas,
      "text-background-opacity": 0.95,
      "text-background-padding": 3,
      "text-background-shape": "roundrectangle",
      "text-rotation": "autorotate",
    },
  },
]

export default {
  mounted() {
    const colors = palette()

    this.cy = cytoscape({
      container: this.el,
      minZoom: 0.25,
      maxZoom: 2.5,
      wheelSensitivity: 0.25,
      boxSelectionEnabled: false,
      autounselectify: true,
      style: stylesheet(colors),
    })

    this.cy.on("tap", "node[kind = 'pop']", event => {
      this.pushEvent("select", {kind: "node", id: event.target.id()})
    })

    this.cy.on("tap", "edge", event => {
      this.pushEvent("select", {kind: "link", id: event.target.data("link")})
    })

    this.cy.on("tap", event => {
      if (event.target === this.cy) this.pushEvent("clear", {})
    })

    // the palette lives in css variables, so a theme flip needs a restyle
    this.onTheme = () => {
      this.cy.style(stylesheet(palette()))
      this.paint()
    }
    window.addEventListener("phx:set-theme", this.onTheme)

    this.observer = new ResizeObserver(() => this.cy.resize())
    this.observer.observe(this.el)

    this.render(true)
  },

  updated() {
    this.render(false)
  },

  destroyed() {
    window.removeEventListener("phx:set-theme", this.onTheme)
    if (this.observer) this.observer.disconnect()
    if (this.cy) this.cy.destroy()
  },

  render(first) {
    const graph = JSON.parse(this.el.dataset.graph)
    this.graph = graph

    const next = signature(graph)

    if (first || next !== this.signature) {
      this.signature = next
      this.cy.elements().remove()
      this.cy.add(this.elements(graph))
      this.relayout()
    }

    this.paint()
  },

  elements(graph) {
    const nodes = graph.nodes.map(node => ({
      group: "nodes",
      data: {
        id: node.id,
        kind: "pop",
        name: node.name,
        status: node.status,
        locator: node.locator,
        sites: node.sites,
        tenants: node.tenants,
      },
    }))

    const edges = graph.edges.map(edge => ({
      group: "edges",
      data: {
        id: edge.id,
        source: edge.local,
        target: edge.remote,
        link: edge.link,
        up: edge.up,
        cost: edge.cost,
      },
    }))

    return [...nodes, ...edges]
  },

  relayout() {
    if (this.cy.nodes().empty()) return

    this.cy
      .layout({
        name: "fcose",
        // randomize is what enables fcose's spectral placement, and that is the
        // whole reason to prefer it over cose. With it off, fcose only refines
        // the positions the nodes already have -- and freshly added nodes all
        // sit on the same coordinate, which collapses the graph into a line.
        randomize: true,
        quality: "proof",
        animate: false,
        fit: false,
        nodeDimensionsIncludeLabels: true,
        // the cards are wide, so they need real separation
        nodeSeparation: 150,
        idealEdgeLength: 250,
        nodeRepulsion: 9000,
        edgeElasticity: 0.4,
        gravity: 0.15,
        gravityRange: 3.8,
        numIter: 3000,
      })
      .run()

    this.fit()
  },

  // never zoom past 1:1 -- a card scaled up looks broken, so pan instead
  fit() {
    this.cy.fit(this.cy.nodes(), 32)

    if (this.cy.zoom() > 1) {
      this.cy.zoom(1)
      this.cy.center(this.cy.nodes())
    }
  },

  paint() {
    const graph = this.graph
    const traced = new Set(graph.trace || [])
    const tracing = traced.size > 0 && graph.overlay === "path"
    const colors = palette()
    const ceiling = Math.max(graph.ceiling || 1, 1)
    const picked = graph.selection || {}

    this.cy.batch(() => {
      this.cy.elements().removeClass("traced picked dimmed labelled")

      this.cy.nodes("[kind = 'pop']").forEach(node => {
        node.style(
          "border-color",
          graph.overlay === "health" ? statusColor(colors, node.data("status")) : colors.line,
        )

        if (traced.has(node.id())) node.addClass("traced")
        else if (tracing) node.addClass("dimmed")

        if (picked.kind === "node" && picked.id === node.id()) node.addClass("picked")
      })

      this.cy.edges().forEach(edge => {
        const onPath = tracing && this.adjacentOnPath(graph.trace, edge)

        if (edge.data("up") === false) {
          // a down adjacency keeps its own colour in every overlay
        } else if (onPath) {
          edge.addClass("traced")
        } else if (tracing) {
          edge.addClass("dimmed")
        } else if (graph.overlay === "cost") {
          const step = costStep((edge.data("cost") || 0) / ceiling)
          edge.style({"line-color": colors.accent, width: step.width, opacity: step.opacity})
          edge.addClass("labelled")
        } else {
          edge.style({"line-color": colors.lineStrong, width: 2, opacity: 1})
        }

        if (picked.kind === "link" && picked.id === edge.data("link")) edge.addClass("picked")
      })
    })
  },

  adjacentOnPath(chain, edge) {
    const source = edge.data("source")
    const target = edge.data("target")

    for (let i = 0; i < chain.length - 1; i++) {
      const a = chain[i]
      const b = chain[i + 1]
      if ((a === source && b === target) || (a === target && b === source)) return true
    }

    return false
  },
}
