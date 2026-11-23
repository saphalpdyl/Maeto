defmodule MaetoPaneWeb.Pane.ChangesTable do
  @moduledoc """
  Reroutes and gated near-misses, newest first, with the edge whose cost moved.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :changes, :list, required: true
  attr :selection, :any, required: true

  def changes_table(assigns) do
    ~H"""
    <table class="w-full min-w-[640px] border-collapse text-left text-[13px]">
      <thead class="border-b border-line text-xs font-medium text-muted">
        <tr>
          <th class="px-4 py-2.5">When</th>
          <th class="py-2.5">Pair</th>
          <th class="py-2.5">Change</th>
          <th class="py-2.5">Because</th>
          <th class="px-4 py-2.5 text-right">Cost</th>
        </tr>
      </thead>
      <tbody>
        <%= for group <- groups(@changes) do %>
          <%= case group do %>
            <% {:single, change} -> %>
              <.change_row change={change} id={"change-#{row_key(change)}"} />
            <% {:gated, key, run} -> %>
              <tr
                id={"gated-#{key}"}
                phx-click={
                  JS.toggle_class("hidden", to: ".gated-#{key}")
                  |> JS.toggle_class("rotate-90", to: "#gated-#{key}-chevron")
                }
                class="cursor-pointer border-b border-line hover:bg-sunken/70"
              >
                <td class="px-4 py-2.5 font-mono text-xs whitespace-nowrap text-muted">
                  {clock(List.first(run).at)}
                </td>
                <td class="py-2.5 whitespace-nowrap text-muted">
                  {pairs(run)}
                </td>
                <td class="py-2.5 font-mono text-xs text-muted">
                  <.pill tone={kind_tone("gated")}>gated</.pill>
                  <span class="ml-2">{length(run)} changes</span>
                  <span class="ml-1 text-faint">since {clock(List.last(run).at)}</span>
                </td>
                <td class="py-2.5 text-xs text-muted"></td>
                <td class="px-4 py-2.5 text-right">
                  <span id={"gated-#{key}-chevron"} class="inline-flex transition-transform">
                    <.icon name="hero-chevron-right" class="size-4 text-faint" />
                  </span>
                </td>
              </tr>
              <.change_row
                :for={change <- run}
                change={change}
                id={"change-#{row_key(change)}"}
                class={"hidden gated-#{key} bg-sunken/40"}
              />
          <% end %>
        <% end %>
        <tr :if={@changes == []}>
          <td class="px-4 py-6 text-center text-xs text-faint" colspan="5">
            No path changes recorded yet
          </td>
        </tr>
      </tbody>
    </table>
    """
  end

  attr :change, :map, required: true
  attr :id, :string, required: true
  attr :class, :string, default: nil

  defp change_row(assigns) do
    ~H"""
    <tr id={@id} class={["border-b border-line last:border-0", @class]}>
      <td class="px-4 py-2.5 font-mono text-xs whitespace-nowrap text-muted">
        {clock(@change.at)}
      </td>
      <td class="py-2.5 whitespace-nowrap">
        <span class="font-mono font-medium text-accent">{@change.src} &rarr; {@change.dst}</span>
        <.class_tag color={@change.slot} name={@change.class} class="ml-2 text-xs text-muted" />
      </td>
      <td class="py-2.5 font-mono text-xs text-muted">
        <.pill tone={kind_tone(@change.kind)}>{@change.kind}</.pill>
        <span :if={@change.from_nodes != []} class="ml-2">
          {Enum.join(@change.from_nodes, " ")} &rarr;
        </span>
        <span class="text-ink">{Enum.join(@change.to_nodes, " ")}</span>
      </td>
      <td class="py-2.5 text-xs text-muted">
        <%= case @change.causes do %>
          <% [] -> %>
            <span class="text-faint">-</span>
          <% [lead | rest] -> %>
            <span class="font-mono text-ink">{lead.edge}</span>
            {num(lead.was)} &rarr; {num(lead.now)}
            <span class="text-faint">({pct(lead.share)})</span>
            <span :if={rest != []} class="text-faint">+{length(rest)} more</span>
        <% end %>
      </td>
      <td class="px-4 py-2.5 text-right font-mono tabular-nums whitespace-nowrap">
        <span :if={@change.from_cost} class="text-muted">{num(@change.from_cost)} &rarr;</span>
        {num(@change.to_cost)}
      </td>
    </tr>
    """
  end

  defp groups(changes) do
    changes
    |> Enum.chunk_by(&(&1.kind == "gated"))
    |> Enum.flat_map(fn
      [%{kind: "gated"} | [_ | _]] = run -> [{:gated, row_key(List.last(run)), run}]
      run -> Enum.map(run, &{:single, &1})
    end)
  end

  defp row_key(change),
    do: :erlang.phash2({change.at, change.tenant, change.src, change.dst, change.label})

  defp pairs(run) do
    case run |> Enum.map(&{&1.src, &1.dst}) |> Enum.uniq() do
      [{src, dst}] -> "#{src} → #{dst}"
      pairs -> "#{length(pairs)} pairs"
    end
  end
end
