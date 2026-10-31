defmodule MaetoPaneWeb.SimLive do
  @moduledoc """
  The demo simulator: lets a visitor impair or cut core links, run traffic and
  play canned stories against the live testbed, all through maeto-sim, which
  bounds every fault and reverts it on a timer.
  """

  use MaetoPaneWeb, :live_view

  alias MaetoPane.Pane
  alias MaetoPane.Pane.Analysis
  alias MaetoPane.Sim
  alias MaetoPaneWeb.Pane.Nav

  import MaetoPaneWeb.Pane.Nav
  import MaetoPaneWeb.Pane.Sim
  import MaetoPaneWeb.Pane.UI

  @tick_ms 1_000
  @stale_after_s 10
  @min_gap_ms 1_000

  @impair_fields ~w(delay_ms jitter_ms loss_pct rate_mbps direction ttl_s)

  @impl true
  def mount(_params, _session, socket) do
    if connected?(socket) do
      Pane.subscribe()
      Sim.subscribe()
      :timer.send_interval(@tick_ms, :tick)
    end

    {:ok,
     socket
     |> assign(:selected, nil)
     |> assign(:busy, false)
     |> assign(:last_command_at, nil)
     |> assign(:page_title, "Simulator")
     |> load_nav()
     |> load_sim()}
  end

  @impl true
  def handle_event("select", %{"id" => id}, socket), do: {:noreply, assign(socket, :selected, id)}
  def handle_event("clear", _params, socket), do: {:noreply, assign(socket, :selected, nil)}

  def handle_event("impair", params, socket) do
    request =
      params
      |> Map.take(["link" | @impair_fields])
      |> Map.put("action", "impair")

    {:noreply, send_command(socket, request)}
  end

  def handle_event("down", %{"link" => link} = params, socket) do
    {:noreply, send_command(socket, %{action: "down", link: link, ttl_s: params["ttl_s"]})}
  end

  def handle_event("clear_fault", %{"link" => link}, socket),
    do: {:noreply, send_command(socket, %{action: "clear", link: link})}

  def handle_event("story_start", %{"name" => name}, socket),
    do: {:noreply, send_command(socket, %{action: "story_start", name: name})}

  def handle_event("story_stop", _params, socket),
    do: {:noreply, send_command(socket, %{action: "story_stop"})}

  def handle_event("scenario_start", %{"name" => name}, socket),
    do: {:noreply, send_command(socket, %{action: "scenario_start", name: name})}

  def handle_event("scenario_stop", _params, socket),
    do: {:noreply, send_command(socket, %{action: "scenario_stop"})}

  def handle_event("reset", _params, socket),
    do: {:noreply, send_command(socket, %{action: "reset"})}

  @impl true
  def handle_info(:tick, socket), do: {:noreply, assign(socket, :now, now())}
  def handle_info(:sim_updated, socket), do: {:noreply, load_sim(socket)}
  def handle_info(:pane_updated, socket), do: {:noreply, load_nav(socket)}

  @impl true
  def handle_async(:command, {:ok, :ok}, socket), do: {:noreply, assign(socket, :busy, false)}

  def handle_async(:command, {:ok, {:error, reason}}, socket) do
    {:noreply, socket |> assign(:busy, false) |> put_flash(:error, reason)}
  end

  def handle_async(:command, {:exit, reason}, socket) do
    {:noreply, socket |> assign(:busy, false) |> put_flash(:error, inspect(reason))}
  end

  defp send_command(%{assigns: %{busy: true}} = socket, _request), do: socket

  defp send_command(socket, request) do
    at = System.monotonic_time(:millisecond)
    last = socket.assigns.last_command_at

    if last && at - last < @min_gap_ms do
      put_flash(socket, :error, "One change a second, please")
    else
      socket
      |> clear_flash()
      |> assign(busy: true, last_command_at: at)
      |> start_async(:command, fn -> Sim.command(request) end)
    end
  end

  defp load_nav(socket) do
    graph = Analysis.graph(Pane.snapshot())

    socket
    |> assign(:nav, Nav.groups(nil, graph.nodes))
    |> assign(:domain, graph.domain["locator_prefix"])
    |> assign(:graph_readiness, graph.readiness)
  end

  defp load_sim(socket) do
    state = Sim.snapshot() || %{}
    links = state["links"] || []

    socket
    |> assign(:now, now())
    |> assign(:sim, state)
    |> assign(:links, links)
    |> assign(:links_by_id, Map.new(links, &{&1["id"], &1}))
    |> assign(:faults, Map.new(state["faults"] || [], &{&1["link"], &1}))
    |> assign(:limits, state["limits"] || %{})
  end

  defp now, do: System.os_time(:millisecond) / 1000

  defp online?(sim, now),
    do: is_number(sim["updated_at"]) and now - sim["updated_at"] < @stale_after_s

  @impl true
  def render(assigns) do
    ~H"""
    <Layouts.app flash={@flash}>
      <:rail>
        <.nav groups={@nav} mode={:navigate} active={:sim} />
      </:rail>

      <:foot>
        <div :if={@domain} class="px-1.5 font-mono text-[10.5px] text-faint">{@domain}</div>
      </:foot>

      <:crumbs>
        <span class="shrink-0 text-[13px] text-muted">Control plane</span>
        <span class="shrink-0 text-faint">&rsaquo;</span>
        <.link navigate={~p"/"} class="shrink-0 text-[13px] text-muted hover:text-ink">Pane</.link>
        <span class="shrink-0 text-faint">&rsaquo;</span>
        <span class="shrink-0 text-[13px] font-medium">Simulator</span>
      </:crumbs>

      <:status>
        <.pce_status readiness={@graph_readiness} />
        <.pill tone={if online?(@sim, @now), do: "ok", else: "warn"}>
          {if online?(@sim, @now), do: "Simulator live", else: "Simulator offline"}
        </.pill>
      </:status>

      <:inspector>
        <%= if link = @selected && @links_by_id[@selected] do %>
          <.link_panel
            link={link}
            fault={@faults[@selected]}
            limits={@limits}
            now={@now}
            busy={@busy}
          />
        <% else %>
          <.overview
            faults={Map.values(@faults)}
            links={@links_by_id}
            limits={@limits}
            now={@now}
          />
        <% end %>
      </:inspector>

      <header class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line px-6 py-4">
        <div class="min-w-0 flex-1">
          <h1 class="text-[15px] font-medium">Simulator</h1>
          <p class="text-[13px] text-muted">
            Inject faults and traffic into the running testbed and watch the control plane react
          </p>
        </div>
        <.action
          kind={:quiet}
          phx-click="reset"
          data-confirm="Revert every fault and stop all traffic and stories?"
          disabled={@busy || !online?(@sim, @now)}
        >
          <.icon name="hero-arrow-path" class="size-3.5" /> Reset testbed
        </.action>
      </header>

      <p
        :if={!online?(@sim, @now)}
        class="border-b border-line bg-warn-soft px-6 py-2.5 text-[13px] text-warn-ink"
      >
        maeto-sim is not publishing. Start it with <span class="font-mono">make sim</span>.
      </p>
      <p
        :if={@sim["error"]}
        class="border-b border-line bg-bad-soft px-6 py-2.5 text-[13px] text-bad-ink"
      >
        {@sim["error"]}
      </p>

      <section class="border-b border-line px-6 py-5">
        <h2 class="mb-3 text-sm font-semibold">Stories</h2>
        <.stories stories={@sim["stories"] || []} story={@sim["story"]} now={@now} busy={@busy} />
      </section>

      <section class="border-b border-line px-6 py-5">
        <h2 class="mb-3 text-sm font-semibold">Traffic</h2>
        <.traffic
          scenarios={@sim["scenarios"] || []}
          scenario={@sim["scenario"]}
          last={@sim["last_scenario"]}
          max_s={@limits["scenario_max_s"] || 0}
          now={@now}
          busy={@busy}
        />
      </section>

      <section>
        <div class="flex items-center gap-3 px-6 pt-5 pb-3">
          <h2 class="text-sm font-semibold">Core links</h2>
          <p class="text-[13px] text-faint">Select a link to impair or cut it</p>
          <span class="ml-auto font-mono text-xs text-muted">
            {map_size(@faults)}/{length(@links)} faulted
          </span>
        </div>
        <div class="overflow-x-auto border-t border-line">
          <.links links={@links} faults={@faults} selected={@selected} now={@now} busy={@busy} />
        </div>
      </section>
    </Layouts.app>
    """
  end
end
