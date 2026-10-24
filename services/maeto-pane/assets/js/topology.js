import {Graph} from "../vendor/x6.min.js"

// The topology is the one surface LiveView does not own: X6 keeps its own svg,
// so the element carries phx-update="ignore" and every change arrives as a JSON
// payload on data-graph. LiveView merges data-* attributes even on an ignored
// element and calls updated() when the dataset changes.
//
// Layout only re-runs when the node/link set changes. Overlay and selection
// changes are restyles of the cells already on the canvas.

const R = 22
const GRID = 16
const SPREAD = 7
const MONO = "'Geist Mono', ui-monospace, SFMono-Regular, Menlo, monospace"
const SANS = "Geist, ui-sans-serif, system-ui, -apple-system, sans-serif"

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
  })[status] || colors.faint

Graph.registerNode(
  "maeto-pop",
  {
    width: R * 2,
    height: R * 2,
    markup: [
      {tagName: "circle", selector: "halo"},
      {tagName: "circle", selector: "ring"},
      {tagName: "text", selector: "role"},
      {tagName: "text", selector: "name"},
      {tagName: "text", selector: "facts"},
    ],
    attrs: {
      halo: {cx: R, cy: R, r: R + 10, strokeWidth: 0, fillOpacity: 0.2, fill: "none"},
      ring: {cx: R, cy: R, r: R - 1.5, strokeWidth: 3, cursor: "pointer"},
      role: {
        x: R,
        y: R,
        textAnchor: "middle",
        textVerticalAnchor: "middle",
        fontFamily: MONO,
        fontSize: 11,
        fontWeight: 600,
        pointerEvents: "none",
      },
      name: {
        x: R,
        y: R * 2 + 16,
        textAnchor: "middle",
        textVerticalAnchor: "middle",
        fontFamily: SANS,
        fontSize: 15,
        fontWeight: 700,
        strokeWidth: 5,
        paintOrder: "stroke",
        cursor: "pointer",
      },
      facts: {
        x: R,
        y: R * 2 + 34,
        textAnchor: "middle",
        textVerticalAnchor: "middle",
        fontFamily: MONO,
        fontSize: 11,
        strokeWidth: 5,
        paintOrder: "stroke",
        pointerEvents: "none",
      },
    },
  },
  true,
)

Graph.registerNode(
  "maeto-label",
  {
    markup: [
      {tagName: "rect", selector: "body"},
      {tagName: "text", selector: "label"},
    ],
    attrs: {
      body: {strokeWidth: 1, cursor: "pointer"},
      label: {
        textAnchor: "middle",
        textVerticalAnchor: "middle",
        fontFamily: MONO,
        fontSize: 11,
        pointerEvents: "none",
      },
    },
  },
  true,
)

const signature = graph =>
  JSON.stringify([
    graph.nodes.map(n => n.id).sort(),
    graph.links.map(l => [l.id, l.members.map(m => m.id)]).sort(),
  ])

// Fruchterman-Reingold from a circle seeded by sorted id, so the same mesh always
// lands in the same place across reloads and clients
const layout = (nodes, links, aspect) => {
  const ids = nodes.map(n => n.id).sort()
  const count = ids.length
  const k = 200
  const pos = new Map()

  ids.forEach((id, i) => {
    const angle = (2 * Math.PI * i) / count
    pos.set(id, {x: Math.cos(angle) * k * 1.5, y: Math.sin(angle) * k * 1.5})
  })

  const pairs = links.filter(l => pos.has(l.a) && pos.has(l.b))
  let temperature = k

  for (let step = 0; step < 500; step++) {
    const shift = new Map(ids.map(id => [id, {x: 0, y: 0}]))

    for (let i = 0; i < count; i++) {
      for (let j = i + 1; j < count; j++) {
        const p = pos.get(ids[i])
        const q = pos.get(ids[j])
        const dx = p.x - q.x
        const dy = p.y - q.y
        const d = Math.max(Math.hypot(dx, dy), 0.01)
        const f = (k * k) / d
        shift.get(ids[i]).x += (dx / d) * f
        shift.get(ids[i]).y += (dy / d) * f
        shift.get(ids[j]).x -= (dx / d) * f
        shift.get(ids[j]).y -= (dy / d) * f
      }
    }

    for (const link of pairs) {
      const p = pos.get(link.a)
      const q = pos.get(link.b)
      const dx = p.x - q.x
      const dy = p.y - q.y
      const d = Math.max(Math.hypot(dx, dy), 0.01)
      const f = (d * d) / k
      shift.get(link.a).x -= (dx / d) * f
      shift.get(link.a).y -= (dy / d) * f
      shift.get(link.b).x += (dx / d) * f
      shift.get(link.b).y += (dy / d) * f
    }

    for (const id of ids) {
      const p = pos.get(id)
      const s = shift.get(id)
      s.x -= p.x * 0.05
      s.y -= p.y * 0.05
      const d = Math.max(Math.hypot(s.x, s.y), 0.01)
      const move = Math.min(d, temperature)
      p.x += (s.x / d) * move
      p.y += (s.y / d) * move
    }

    temperature = Math.max(temperature * 0.985, 1)
  }

  // turn the long axis horizontal, then stretch toward the canvas shape so a
  // wide pane is not left with a tall column
  const points = [...pos.values()]
  const cx = points.reduce((sum, p) => sum + p.x, 0) / count
  const cy = points.reduce((sum, p) => sum + p.y, 0) / count
  let sxx = 0
  let syy = 0
  let sxy = 0
  for (const p of points) {
    sxx += (p.x - cx) ** 2
    syy += (p.y - cy) ** 2
    sxy += (p.x - cx) * (p.y - cy)
  }
  const theta = -0.5 * Math.atan2(2 * sxy, sxx - syy)
  for (const p of points) {
    const x = p.x - cx
    const y = p.y - cy
    p.x = x * Math.cos(theta) - y * Math.sin(theta)
    p.y = x * Math.sin(theta) + y * Math.cos(theta)
  }

  const spanX = Math.max(...points.map(p => p.x)) - Math.min(...points.map(p => p.x)) || 1
  const spanY = Math.max(...points.map(p => p.y)) - Math.min(...points.map(p => p.y)) || 1
  const stretch = Math.min(Math.max(Math.sqrt(aspect / (spanX / spanY)), 0.6), 2.2)

  for (const p of pos.values()) {
    p.x *= stretch
    p.y /= stretch
    p.x = Math.round(p.x / 20) * 20
    p.y = Math.round(p.y / 20) * 20
  }

  return pos
}

const latencyText = value => {
  if (value == null) return "–"
  return value >= 10 ? String(Math.round(value)) : value.toFixed(1)
}

const labelText = link => {
  const up = link.members.filter(m => m.up).length
  const total = link.members.length
  return up === 0 ? `∞ · 0/${total}` : `${latencyText(link.latency)} · ${up}/${total}`
}

export default {
  mounted() {
    this.graph = new Graph({
      container: this.el,
      autoResize: false,
      width: this.el.clientWidth,
      height: this.el.clientHeight,
      grid: {visible: true, size: GRID, type: "dot", args: {color: token("--mt-line-strong"), thickness: 1.5}},
      background: {color: token("--mt-surface")},
      panning: {enabled: true},
      mousewheel: {enabled: true, factor: 1.1, minScale: 0.4, maxScale: 2, zoomAtMousePosition: true},
      interacting: false,
    })

    this.graph.on("node:click", ({node}) => {
      const data = node.getData() || {}
      if (data.kind === "pop") this.pushEvent("select", {kind: "node", id: node.id})
      if (data.kind === "label") this.pushEvent("select", {kind: "link", id: data.link})
    })

    this.graph.on("edge:click", ({edge}) => {
      this.pushEvent("select", {kind: "link", id: edge.getData().link})
    })

    this.graph.on("blank:click", () => this.pushEvent("clear", {}))

    // the palette lives in css variables, so a theme flip needs a full redraw
    this.onTheme = () => requestAnimationFrame(() => this.render(true))
    window.addEventListener("phx:set-theme", this.onTheme)

    this.observer = new ResizeObserver(() => {
      this.graph.resize(this.el.clientWidth, this.el.clientHeight)
    })
    this.observer.observe(this.el)

    this.render(true)
  },

  updated() {
    this.render(false)
  },

  destroyed() {
    window.removeEventListener("phx:set-theme", this.onTheme)
    if (this.observer) this.observer.disconnect()
    if (this.graph) this.graph.dispose()
  },

  render(full) {
    const payload = JSON.parse(this.el.dataset.graph)
    this.payload = payload

    const next = signature(payload)

    if (full || next !== this.signature) {
      const relayout = next !== this.signature || !this.positions
      this.signature = next
      if (relayout) this.positions = layout(payload.nodes, payload.links, this.el.clientWidth / Math.max(this.el.clientHeight, 1))
      this.draw(payload)
      if (relayout) this.graph.zoomToFit({padding: 56, maxScale: 1})
    }

    this.paint()
  },

  draw(payload) {
    const colors = palette()
    const pos = this.positions

    this.graph.drawGrid({type: "dot", args: {color: colors.lineStrong, thickness: 1.5}})
    this.graph.drawBackground({color: colors.surface})

    const cells = []

    for (const link of payload.links) {
      const p = pos.get(link.a)
      const q = pos.get(link.b)
      if (!p || !q) continue

      const dx = q.x - p.x
      const dy = q.y - p.y
      const d = Math.max(Math.hypot(dx, dy), 1)
      const ux = dx / d
      const uy = dy / d
      const trim = R + 2
      const n = link.members.length

      link.members.forEach((member, i) => {
        const off = (i - (n - 1) / 2) * SPREAD
        const ox = -uy * off
        const oy = ux * off

        cells.push(
          this.graph.createEdge({
            id: `m:${member.id}`,
            source: {x: p.x + ux * trim + ox, y: p.y + uy * trim + oy},
            target: {x: q.x - ux * trim + ox, y: q.y - uy * trim + oy},
            zIndex: 1,
            data: {link: link.id, up: member.up},
            attrs: {
              line: {targetMarker: null, strokeLinecap: "round"},
              wrap: {cursor: "pointer"},
            },
          }),
        )
      })

      const text = labelText(link)
      const width = text.length * 7 + 18

      cells.push(
        this.graph.createNode({
          id: `l:${link.id}`,
          shape: "maeto-label",
          x: (p.x + q.x) / 2 - width / 2,
          y: (p.y + q.y) / 2 - 11,
          width,
          height: 22,
          zIndex: 2,
          data: {kind: "label", link: link.id},
          attrs: {
            body: {width, height: 22, rx: 2},
            label: {x: width / 2, y: 11, text},
          },
        }),
      )
    }

    for (const node of payload.nodes) {
      const p = pos.get(node.id)
      if (!p) continue

      cells.push(
        this.graph.createNode({
          id: node.id,
          shape: "maeto-pop",
          x: p.x - R,
          y: p.y - R,
          zIndex: 3,
          data: {kind: "pop"},
          attrs: {
            role: {text: node.role},
            name: {text: node.name || node.id},
            facts: {text: `${node.vpns} vpn · ${node.ifs} if`},
          },
        }),
      )
    }

    this.graph.resetCells(cells)
  },

  paint() {
    const payload = this.payload
    const colors = palette()
    const overlay = payload.overlay
    const trace = payload.trace || []
    const tracing = overlay === "path" && trace.length > 0
    const traced = new Set(trace)
    const hops = new Set()
    for (let i = 0; i < trace.length - 1; i++) hops.add([trace[i], trace[i + 1]].sort().join("|"))
    const ceiling = Math.max(payload.ceiling || 1, 1)
    const picked = payload.selection || {}

    this.graph.batchUpdate(() => {
      for (const node of payload.nodes) {
        const cell = this.graph.getCellById(node.id)
        if (!cell) continue

        const isPicked = picked.kind === "node" && picked.id === node.id
        const status = statusColor(colors, node.status)
        const ring = overlay === "health" ? status : tracing && traced.has(node.id) ? colors.accent : colors.lineStrong
        const alarm = overlay === "health" && node.status === "issue"
        const dim = tracing && !traced.has(node.id)

        const opacity = dim ? 0.3 : 1

        cell.attr({
          halo: {fill: isPicked ? colors.accent : alarm ? colors.bad : "none"},
          ring: {stroke: isPicked ? colors.accent : ring, fill: colors.surface, opacity},
          role: {fill: isPicked ? colors.accent : ring, opacity},
          name: {fill: isPicked ? colors.accent : colors.ink, stroke: colors.surface, opacity},
          facts: {fill: isPicked ? colors.accent : colors.muted, stroke: colors.surface, opacity},
        })
      }

      for (const link of payload.links) {
        const isPicked = picked.kind === "link" && picked.id === link.id
        const onPath = tracing && hops.has([link.a, link.b].sort().join("|"))
        const dim = tracing && !onPath
        const up = link.members.filter(m => m.up).length
        const ratio = link.latency == null ? 0 : link.latency / ceiling

        for (const member of link.members) {
          const edge = this.graph.getCellById(`m:${member.id}`)
          if (!edge) continue

          let stroke = colors.lineStrong
          let width = 2
          let opacity = 1

          if (!member.up) {
            stroke = colors.bad
          } else if (isPicked || onPath) {
            stroke = colors.accent
            width = 3
          } else if (overlay === "cost") {
            stroke = colors.accent
            width = 1.5 + ratio * 3
            opacity = 0.35 + ratio * 0.65
          }

          if (dim) opacity = 0.15

          edge.attr("line", {
            stroke,
            strokeWidth: width,
            strokeOpacity: opacity,
            strokeDasharray: member.up ? null : "6 4",
          })
        }

        const label = this.graph.getCellById(`l:${link.id}`)
        if (!label) continue

        let tone = colors.muted
        if (up === 0) tone = colors.bad
        else if (up < link.members.length) tone = colors.warn
        else if (isPicked || onPath || overlay === "cost") tone = colors.accent

        label.attr({
          body: {
            fill: colors.surface,
            stroke: isPicked ? colors.accent : up === 0 ? tone : colors.line,
            strokeWidth: isPicked ? 1.5 : 1,
            opacity: dim ? 0.3 : 1,
          },
          label: {fill: tone, opacity: dim ? 0.3 : 1},
        })
      }
    })
  },
}
