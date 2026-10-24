defmodule MaetoPaneWeb.Pane.UI do
  @moduledoc """
  The pane console's building blocks: pills, segment chains, inspector
  sections and the small formatters every table and panel shares.
  """

  use MaetoPaneWeb, :html

  def selected?(nil, _kind, _id), do: false
  def selected?({kind, id}, kind, id), do: true
  def selected?(_selection, _kind, _id), do: false

  def dot("ok"), do: "bg-ok"
  def dot("warn"), do: "bg-warn"
  def dot("bad"), do: "bg-bad"
  def dot("idle"), do: "bg-idle"
  def dot("accent"), do: "bg-accent"
  def dot(_tone), do: "bg-faint"

  def pill_classes("ok"), do: "bg-ok-soft text-ok-ink"
  def pill_classes("warn"), do: "bg-warn-soft text-warn-ink"
  def pill_classes("bad"), do: "bg-bad-soft text-bad-ink"
  def pill_classes("idle"), do: "bg-idle-soft text-idle"
  def pill_classes("accent"), do: "bg-accent-soft text-ok-ink"
  def pill_classes("steer"), do: "bg-steer-soft text-steer-ink"
  def pill_classes(_tone), do: "bg-sunken text-muted"

  def num(nil), do: "-"
  def num(value) when is_float(value), do: :erlang.float_to_binary(value, decimals: 1)
  def num(value), do: to_string(value)

  def clock(nil), do: "-"

  def clock(stamp) when is_binary(stamp) do
    case DateTime.from_iso8601(stamp) do
      {:ok, at, _offset} -> Calendar.strftime(at, "%H:%M:%S")
      _ -> stamp
    end
  end

  def pct(nil), do: "-"
  def pct(share), do: "#{round(share * 100)}%"

  def hop_chip(:decap), do: "bg-dt46 text-dt46-ink"
  def hop_chip(:transit), do: "bg-endx text-endx-ink"
  def hop_chip(:end_x), do: "bg-endx text-endx-ink"
  def hop_chip(_role), do: "bg-sunken text-muted"

  def hop_text(:decap), do: "text-accent"
  def hop_text(role) when role in [:transit, :end_x], do: "text-steer"
  def hop_text(_role), do: "text-faint"

  def hop_behavior(:decap), do: "end.dt46"
  def hop_behavior(:end_x), do: "end.x"
  def hop_behavior(_role), do: "end"

  attr :value, :any, required: true
  attr :label, :string, required: true
  attr :bad, :boolean, default: false

  def tally(assigns) do
    ~H"""
    <span class="flex items-center gap-1.5 whitespace-nowrap">
      <span class={["font-mono tabular-nums", if(@bad, do: "text-bad", else: "text-ink")]}>
        {@value}
      </span>
      <span>{@label}</span>
    </span>
    """
  end

  attr :tone, :any, default: "faint"
  slot :inner_block, required: true

  def pill(assigns) do
    ~H"""
    <span class={[
      "inline-flex shrink-0 items-center gap-1.5 rounded-full px-2.5 py-px text-[13px] leading-5 font-medium whitespace-nowrap",
      pill_classes(@tone)
    ]}>
      <span class={["size-1.5 rounded-full", dot(@tone)]} />
      {render_slot(@inner_block)}
    </span>
    """
  end

  attr :hops, :list, required: true

  def chain(assigns) do
    ~H"""
    <div class="flex flex-col items-start gap-1">
      <span :if={@hops == []} class="text-xs text-faint">no segments</span>
      <span :for={{hop, index} <- Enum.with_index(@hops)} class="inline-flex items-center gap-1.5">
        <span
          class={[
            "inline-flex items-center gap-1.5 rounded-[5px] px-1.5 py-0.5 font-mono text-[12px]",
            hop_chip(hop.role)
          ]}
          title={hop.node_name || "no locator owns this segment"}
        >
          {hop.sid}
          <span class="text-[10px] tracking-wide uppercase opacity-75">
            {hop_behavior(hop.role)}
          </span>
        </span>
        <span :if={index < length(@hops) - 1} class="text-[11px] text-faint">&rarr;</span>
      </span>
    </div>
    """
  end

  attr :title, :string, required: true
  attr :caption, :string, default: nil
  slot :aside

  def card_head(assigns) do
    ~H"""
    <div class="flex flex-wrap items-center gap-x-2.5 gap-y-1 border-b border-line px-4 py-3">
      <h2 class="text-sm font-semibold">{@title}</h2>
      <p :if={@caption} class="text-[13px] text-faint">{@caption}</p>
      <div :if={@aside != []} class="ml-auto flex items-center gap-2">{render_slot(@aside)}</div>
    </div>
    """
  end

  attr :tone, :any, default: "faint"
  slot :inner_block, required: true

  def stat(assigns) do
    ~H"""
    <span class={[
      "rounded-md px-2 py-0.5 font-mono text-xs font-medium whitespace-nowrap",
      pill_classes(@tone)
    ]}>
      {render_slot(@inner_block)}
    </span>
    """
  end

  attr :title, :string, required: true
  attr :subtitle, :string, default: nil
  attr :tone, :any, default: nil
  attr :state, :any, default: nil

  def head(assigns) do
    ~H"""
    <header class="flex items-start gap-2.5 border-b border-line px-4 py-3">
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <h2 class="min-w-0 truncate text-sm font-semibold">{@title}</h2>
          <.pill :if={@state} tone={@tone || "faint"}>{@state}</.pill>
        </div>
        <p :if={@subtitle} class="mt-0.5 truncate font-mono text-[11px] text-muted">{@subtitle}</p>
      </div>
      <.esc />
    </header>
    """
  end

  def esc(assigns) do
    ~H"""
    <button
      phx-click="clear"
      class="shrink-0 rounded-md border border-line bg-surface px-2 py-0.5 text-xs text-muted transition-colors hover:text-ink"
    >
      Esc
    </button>
    """
  end

  attr :title, :string, required: true
  slot :inner_block, required: true

  def group(assigns) do
    ~H"""
    <section class="border-b border-line px-4 py-3 last:border-0">
      <h3 class="mb-2 text-[13px] font-medium">{@title}</h3>
      {render_slot(@inner_block)}
    </section>
    """
  end

  attr :label, :string, required: true
  attr :value, :any, default: nil

  def row(assigns) do
    ~H"""
    <div class="flex justify-between gap-3 text-xs">
      <dt class="shrink-0 text-muted">{@label}</dt>
      <dd class="truncate font-mono">{@value || "-"}</dd>
    </div>
    """
  end
end
