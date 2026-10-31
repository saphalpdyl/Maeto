defmodule MaetoPaneWeb.Pane.Sim do
  @moduledoc """
  The simulator page's pieces: canned stories, traffic scenarios, the core
  links with whatever fault is on them, and the inspector form that breaks one.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.UI

  @ttls [{"30 s", 30}, {"1 min", 60}, {"2 min", 120}, {"5 min", 300}, {"10 min", 600}]

  def remaining(nil, _now), do: "-"

  def remaining(until, now) do
    seconds = max(round(until - now), 0)
    "#{div(seconds, 60)}:#{String.pad_leading(Integer.to_string(rem(seconds, 60)), 2, "0")}"
  end

  def duration(seconds) when seconds in [0, nil], do: "endless"
  def duration(seconds), do: remaining(seconds, 0)

  def fault_summary(%{"kind" => "down"}), do: "link down"

  def fault_summary(fault) do
    [
      fault["delay_ms"] > 0 && "+#{trim(fault["delay_ms"])} ms",
      fault["jitter_ms"] > 0 && "±#{trim(fault["jitter_ms"])} ms",
      fault["loss_pct"] > 0 && "#{trim(fault["loss_pct"])}% loss",
      fault["rate_mbps"] && "#{trim(fault["rate_mbps"])} Mbps cap"
    ]
    |> Enum.filter(& &1)
    |> Enum.join(" · ")
  end

  def direction_label(%{"kind" => "down"}, _link), do: "both ways"
  def direction_label(%{"direction" => "ab"}, link), do: "#{link["a"]} → #{link["b"]}"
  def direction_label(%{"direction" => "ba"}, link), do: "#{link["b"]} → #{link["a"]}"
  def direction_label(_fault, _link), do: "both ways"

  def source_label("manual"), do: "visitor"
  def source_label("story:" <> _name), do: "story"
  def source_label(source), do: source

  def fault_tone(nil), do: "ok"
  def fault_tone(%{"kind" => "down"}), do: "bad"
  def fault_tone(_fault), do: "warn"

  def fault_state(nil), do: "Normal"
  def fault_state(%{"kind" => "down"}), do: "Down"
  def fault_state(_fault), do: "Impaired"

  defp trim(value) when is_float(value) and value == trunc(value), do: trunc(value)
  defp trim(value), do: value

  attr :class, :any, default: nil
  attr :kind, :atom, default: :primary, values: [:primary, :danger, :quiet]
  attr :rest, :global, include: ~w(disabled type form name value)
  slot :inner_block, required: true

  def action(assigns) do
    ~H"""
    <button
      class={[
        "inline-flex shrink-0 items-center justify-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium whitespace-nowrap transition-colors disabled:cursor-not-allowed disabled:opacity-50",
        action_classes(@kind),
        @class
      ]}
      {@rest}
    >
      {render_slot(@inner_block)}
    </button>
    """
  end

  defp action_classes(:primary), do: "bg-steer text-surface hover:opacity-90"
  defp action_classes(:danger), do: "bg-bad text-surface hover:opacity-90"

  defp action_classes(:quiet),
    do: "border border-line bg-surface text-muted hover:bg-sunken hover:text-ink"

  attr :stories, :list, required: true
  attr :story, :map, default: nil
  attr :now, :float, required: true
  attr :busy, :boolean, default: false

  def stories(assigns) do
    ~H"""
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2 2xl:grid-cols-3">
      <div
        :for={story <- @stories}
        id={"story-#{story["name"]}"}
        class={[
          "flex flex-col gap-2 rounded-lg border p-3.5",
          if(running?(@story, story),
            do: "border-steer-ring bg-steer-soft",
            else: "border-line bg-surface"
          )
        ]}
      >
        <div class="flex items-start gap-2">
          <h3 class="min-w-0 flex-1 text-[13px] font-semibold">{story["title"]}</h3>
          <span class="font-mono text-[11px] text-faint">{duration(story["duration_s"])}</span>
        </div>
        <p class="flex-1 text-[13px] text-pretty text-muted">{story["description"]}</p>

        <div :if={running?(@story, story)} class="space-y-1">
          <div class="h-1 overflow-hidden rounded-full bg-sunken">
            <div class="h-full bg-steer" style={"width: #{progress(@story, @now)}%"} />
          </div>
          <div class="flex justify-between font-mono text-[11px] text-steer-ink">
            <span>running</span>
            <span>{remaining(@story["ends_at"], @now)} left</span>
          </div>
        </div>

        <div class="flex justify-end">
          <.action
            :if={running?(@story, story)}
            kind={:quiet}
            phx-click="story_stop"
            disabled={@busy}
          >
            <.icon name="hero-stop" class="size-3.5" /> Stop and revert
          </.action>
          <.action
            :if={!running?(@story, story)}
            phx-click="story_start"
            phx-value-name={story["name"]}
            disabled={@busy || @story != nil}
          >
            <.icon name="hero-play" class="size-3.5" /> Run
          </.action>
        </div>
      </div>
      <p :if={@stories == []} class="text-[13px] text-muted">No stories for this topology</p>
    </div>
    """
  end

  defp running?(nil, _story), do: false
  defp running?(running, story), do: running["name"] == story["name"]

  defp progress(story, now) do
    total = story["ends_at"] - story["started_at"]
    if total <= 0, do: 100, else: min(round((now - story["started_at"]) / total * 100), 100)
  end

  attr :scenarios, :list, required: true
  attr :scenario, :map, default: nil
  attr :last, :map, default: nil
  attr :max_s, :integer, required: true
  attr :now, :float, required: true
  attr :busy, :boolean, default: false

  def traffic(assigns) do
    ~H"""
    <div class="divide-y divide-line rounded-lg border border-line">
      <div :for={scenario <- @scenarios} id={"scenario-#{scenario["name"]}"} class="px-3.5 py-3">
        <div class="flex items-start gap-3">
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2">
              <h3 class="text-[13px] font-semibold">{scenario["title"]}</h3>
              <span class="font-mono text-[11px] text-faint">{duration(scenario["duration_s"])}</span>
              <.pill :if={running?(@scenario, scenario)} tone="steer">
                running · {remaining(@scenario["expires_at"], @now)} left
              </.pill>
            </div>
            <p class="mt-0.5 text-[13px] text-pretty text-muted">{scenario["description"]}</p>
          </div>

          <.action
            :if={running?(@scenario, scenario)}
            kind={:quiet}
            phx-click="scenario_stop"
            disabled={@busy}
          >
            <.icon name="hero-stop" class="size-3.5" /> Stop
          </.action>
          <.action
            :if={!running?(@scenario, scenario)}
            phx-click="scenario_start"
            phx-value-name={scenario["name"]}
            disabled={@busy || @scenario != nil}
          >
            <.icon name="hero-play" class="size-3.5" /> Start
          </.action>
        </div>

        <pre
          :if={running?(@scenario, scenario) && @scenario["report"] != ""}
          class="mt-2.5 overflow-x-auto rounded-md bg-sunken px-3 py-2 font-mono text-[11px] leading-4 text-muted"
        >{@scenario["report"]}</pre>
      </div>
    </div>
    <p class="mt-2 text-xs text-faint">
      <span :if={@last && @last["exit_code"] not in [0, nil] && @scenario == nil} class="text-bad">
        {@last["name"]} exited with code {@last["exit_code"]}.
      </span>
    </p>
    """
  end

  attr :links, :list, required: true
  attr :faults, :map, required: true
  attr :selected, :any, default: nil
  attr :now, :float, required: true
  attr :busy, :boolean, default: false

  def links(assigns) do
    ~H"""
    <table class="w-full min-w-[640px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs font-medium text-muted">
        <tr>
          <th class="px-4 py-2.5">Link</th>
          <th class="py-2.5">Interfaces</th>
          <th class="py-2.5 text-right">Baseline</th>
          <th class="py-2.5 pl-6">Fault</th>
          <th class="py-2.5 text-right">Reverts in</th>
          <th class="px-4 py-2.5 text-right">State</th>
        </tr>
      </thead>
      <tbody>
        <tr
          :for={link <- @links}
          id={"sim-link-#{link["id"]}"}
          class={[
            "cursor-pointer border-b border-line last:border-0 hover:bg-sunken/70",
            @selected == link["id"] && "bg-accent-soft"
          ]}
          phx-click="select"
          phx-value-id={link["id"]}
        >
          <td class="px-4 py-2.5 font-mono font-medium whitespace-nowrap text-accent">
            {link["label"]}
          </td>
          <td class="py-2.5 font-mono text-xs text-muted">
            {link["a"]}:{link["a_iface"]} &harr; {link["b"]}:{link["b_iface"]}
          </td>
          <td class="py-2.5 text-right font-mono text-xs whitespace-nowrap text-muted tabular-nums">
            {num(link["delay_ms"])} ms · {num(link["bandwidth_mbps"])} Mbps
          </td>
          <td class="py-2.5 pl-6 text-xs">
            <%= if fault = @faults[link["id"]] do %>
              <div class="font-mono">{fault_summary(fault)}</div>
              <div class="text-faint">
                {direction_label(fault, link)} · by {source_label(fault["source"])}
              </div>
            <% else %>
              <span class="text-faint">-</span>
            <% end %>
          </td>
          <td class="py-2.5 text-right font-mono text-xs tabular-nums">
            {if fault = @faults[link["id"]], do: remaining(fault["expires_at"], @now), else: "-"}
          </td>
          <td class="px-4 py-2.5 text-right">
            <.pill tone={fault_tone(@faults[link["id"]])}>{fault_state(@faults[link["id"]])}</.pill>
          </td>
        </tr>
        <tr :if={@links == []}>
          <td colspan="6" class="px-4 py-10 text-center text-muted">No core links</td>
        </tr>
      </tbody>
    </table>
    """
  end

  attr :link, :map, required: true
  attr :fault, :map, default: nil
  attr :limits, :map, required: true
  attr :now, :float, required: true
  attr :busy, :boolean, default: false

  def link_panel(assigns) do
    assigns = assign(assigns, :ttls, @ttls)

    ~H"""
    <.head
      title={@link["label"]}
      subtitle={"#{@link["a"]}:#{@link["a_iface"]} ↔ #{@link["b"]}:#{@link["b_iface"]}"}
      state={fault_state(@fault)}
      tone={fault_tone(@fault)}
    />

    <.group title="Baseline">
      <dl class="space-y-1">
        <.row label="Delay" value={"#{num(@link["delay_ms"])} ms each way"} />
        <.row label="Capacity" value={"#{num(@link["bandwidth_mbps"])} Mbps"} />
      </dl>
    </.group>

    <.group :if={@fault} title="Active fault">
      <dl class="space-y-1">
        <.row label="Fault" value={fault_summary(@fault)} />
        <.row label="Direction" value={direction_label(@fault, @link)} />
        <.row label="Started by" value={source_label(@fault["source"])} />
        <.row label="Reverts in" value={remaining(@fault["expires_at"], @now)} />
      </dl>
      <div class="mt-3 flex justify-end">
        <.action kind={:quiet} phx-click="clear_fault" phx-value-link={@link["id"]} disabled={@busy}>
          <.icon name="hero-arrow-uturn-left" class="size-3.5" /> Revert now
        </.action>
      </div>
    </.group>

    <.group title="Impair">
      <p class="mb-3 text-xs text-pretty text-muted">
        Added on top of the baseline with netem. Leave a field empty to skip it.
      </p>
      <form id="impair-form" phx-submit="impair" class="space-y-2.5">
        <input type="hidden" name="link" value={@link["id"]} />
        <div class="grid grid-cols-2 gap-2.5">
          <.field name="delay_ms" label="Delay" unit="ms" max={@limits["delay_ms"]} />
          <.field name="jitter_ms" label="Jitter" unit="ms" max={@limits["jitter_ms"]} />
          <.field name="loss_pct" label="Loss" unit="%" max={@limits["loss_pct"]} step="0.1" />
          <.field name="rate_mbps" label="Rate cap" unit="Mbps" max={@limits["rate_mbps"]} />
        </div>
        <div class="grid grid-cols-2 gap-2.5">
          <label class="space-y-1 text-xs text-muted">
            <span>Direction</span>
            <select name="direction" class={input_classes()}>
              <option value="both">Both ways</option>
              <option value="ab">{@link["a"]} → {@link["b"]}</option>
              <option value="ba">{@link["b"]} → {@link["a"]}</option>
            </select>
          </label>
          <.ttl_select ttls={@ttls} />
        </div>
        <div class="flex justify-end">
          <.action type="submit" disabled={@busy}>
            <.icon name="hero-bolt" class="size-3.5" /> Apply
          </.action>
        </div>
      </form>
    </.group>

    <.group title="Cut">
      <p class="mb-3 text-xs text-pretty text-muted">
        Takes both ends administratively down, like a fiber cut.
      </p>
      <form id="down-form" phx-submit="down" class="flex items-end gap-2.5">
        <input type="hidden" name="link" value={@link["id"]} />
        <div class="flex-1"><.ttl_select ttls={@ttls} /></div>
        <.action type="submit" kind={:danger} disabled={@busy}>
          <.icon name="hero-scissors" class="size-3.5" /> Take down
        </.action>
      </form>
    </.group>
    """
  end

  attr :name, :string, required: true
  attr :label, :string, required: true
  attr :unit, :string, required: true
  attr :max, :any, required: true
  attr :step, :string, default: "1"

  defp field(assigns) do
    ~H"""
    <label class="space-y-1 text-xs text-muted">
      <span>{@label} <span class="text-faint">({@unit}, max {@max})</span></span>
      <input
        type="number"
        name={@name}
        min="0"
        max={@max}
        step={@step}
        inputmode="decimal"
        class={input_classes()}
      />
    </label>
    """
  end

  attr :ttls, :list, required: true

  defp ttl_select(assigns) do
    ~H"""
    <label class="space-y-1 text-xs text-muted">
      <span>Revert after</span>
      <select name="ttl_s" class={input_classes()}>
        <option :for={{label, seconds} <- @ttls} value={seconds} selected={seconds == 120}>
          {label}
        </option>
      </select>
    </label>
    """
  end

  defp input_classes,
    do:
      "block w-full rounded-md border border-line bg-surface px-2 py-1 font-mono text-[13px] text-ink focus:border-steer focus:outline-none"

  attr :faults, :list, required: true
  attr :links, :map, required: true
  attr :limits, :map, required: true
  attr :now, :float, required: true

  def overview(assigns) do
    ~H"""
    <header class="border-b border-line px-4 py-3">
      <h2 class="text-sm font-semibold">Break the testbed</h2>
      <p class="mt-0.5 text-[13px] text-pretty text-muted">
        Faults are real: netem and link state on the PoP containers. Pick a link to impair or
        cut it, or run a story. Everything reverts on its own timer.
      </p>
    </header>

    <.group title="Active faults">
      <p :if={@faults == []} class="text-xs text-faint">None. The fabric is at its baseline.</p>
      <ul class="space-y-1">
        <li :for={fault <- @faults}>
          <button
            phx-click="select"
            phx-value-id={fault["link"]}
            class="-mx-2 flex w-[calc(100%+1rem)] items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-sunken/70"
          >
            <span class={["size-1.5 shrink-0 rounded-full", dot(fault_tone(fault))]} />
            <span class="font-mono font-medium text-accent">
              {get_in(@links, [fault["link"], "label"]) || fault["link"]}
            </span>
            <span class="min-w-0 flex-1 truncate text-muted">{fault_summary(fault)}</span>
            <span class="font-mono text-faint tabular-nums">
              {remaining(fault["expires_at"], @now)}
            </span>
          </button>
        </li>
      </ul>
    </.group>

    <.group title="Limits">
      <dl class="space-y-1">
        <.row label="Links faulted at once" value={@limits["manual_faults"]} />
        <.row label="Links down at once" value={"#{@limits["manual_down"]}, never isolating a PoP"} />
        <.row label="Delay / jitter" value={"#{@limits["delay_ms"]} / #{@limits["jitter_ms"]} ms"} />
        <.row label="Loss" value={"#{@limits["loss_pct"]}%"} />
        <.row label="Longest fault" value={"#{div(@limits["ttl_max_s"], 60)} min"} />
      </dl>
    </.group>
    """
  end
end
