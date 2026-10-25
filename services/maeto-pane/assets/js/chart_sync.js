// Moves every chart in the container to the same bucket. The server renders
// each chart with its series on data-series; the hook only positions a cursor,
// two dots and the readouts, so a LiveView patch never fights it: updated()
// just reapplies whatever bucket was last hovered.

// viewBox height and the 2px padding the server draws the line with
const PLOT_HEIGHT = 72

const format = (value, unit) => {
  if (value == null) return "–"
  if (value >= 10 || value === Math.round(value)) return `${Math.round(value)}${unit}`
  return `${value.toFixed(1)}${unit}`
}

const pad = n => String(n).padStart(2, "0")

const clock = ms => {
  const at = new Date(ms)
  return `${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`
}

export default {
  mounted() {
    this.index = null

    this.onMove = event => {
      const plot = event.target.closest("[data-plot]")
      if (!plot || !this.el.contains(plot)) return

      const chart = plot.closest("[data-chart]")
      const count = JSON.parse(chart.dataset.series)[0].length
      const rect = plot.getBoundingClientRect()
      const ratio = (event.clientX - rect.left) / rect.width
      const index = Math.min(count - 1, Math.max(0, Math.floor(ratio * count)))

      if (index !== this.index) {
        this.index = index
        this.paint()
      }
    }

    this.onLeave = () => {
      this.index = null
      this.paint()
    }

    this.el.addEventListener("mousemove", this.onMove)
    this.el.addEventListener("mouseleave", this.onLeave)
  },

  updated() {
    this.paint()
  },

  destroyed() {
    this.el.removeEventListener("mousemove", this.onMove)
    this.el.removeEventListener("mouseleave", this.onLeave)
  },

  paint() {
    for (const chart of this.el.querySelectorAll("[data-chart]")) {
      const series = JSON.parse(chart.dataset.series)
      const unit = chart.dataset.unit
      const top = Number(chart.dataset.top) || 1
      const cursor = chart.querySelector("[data-cursor]")
      const time = chart.querySelector("[data-time]")

      if (this.index == null) {
        cursor.classList.add("hidden")
        for (const dot of chart.querySelectorAll("[data-dot]")) dot.classList.add("hidden")
        for (const readout of chart.querySelectorAll("[data-readout]")) {
          readout.textContent = readout.dataset.latest
        }
        if (time) time.textContent = ""
        continue
      }

      const count = series[0].length
      const left = ((this.index + 0.5) / count) * 100

      cursor.style.left = `${left}%`
      cursor.classList.remove("hidden")

      for (const readout of chart.querySelectorAll("[data-readout]")) {
        const values = series[Number(readout.dataset.readout)] || []
        readout.textContent = format(values[this.index], unit)
      }

      for (const dot of chart.querySelectorAll("[data-dot]")) {
        const values = series[Number(dot.dataset.dot)]
        const value = values ? values[this.index] : null

        if (value == null) {
          dot.classList.add("hidden")
          continue
        }

        const fromBottom = 2 + (value / top) * (PLOT_HEIGHT - 4)
        dot.style.left = `${left}%`
        dot.style.bottom = `${(fromBottom / PLOT_HEIGHT) * 100}%`
        dot.classList.remove("hidden")
      }

      if (time) {
        const start = Number(chart.dataset.from) + this.index * Number(chart.dataset.bucket)
        time.textContent = clock(start)
      }
    }
  },
}
