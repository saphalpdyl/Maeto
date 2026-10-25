defmodule MaetoPaneWeb.Pane.Nav do
  @moduledoc """
  The sidebar: the pane's sections grouped by the layer they describe, each with
  a count and, when something is wrong in it, a count in the bad tone. The
  drilldown group lists every PoP with its state.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI, only: [dot: 1]

  attr :groups, :list, required: true
  attr :active, :any, default: nil
  attr :mode, :atom, default: :patch, values: [:patch, :navigate]

  def nav(assigns) do
    ~H"""
    <div class="space-y-1.5">
      <div
        :for={group <- @groups}
        class="pt-1.5 pb-2 last:border-0"
      >
        <div class="flex items-center gap-2.5 px-2 py-1.5 font-medium">
          <.icon name={group.icon} class="size-4 shrink-0 text-muted" />
          <span class="text-[13px]">{group.label}</span>
        </div>

        <div class="ml-[15px] space-y-px border-l-2 border-line pl-2.5">
          <.link
            :for={item <- group.items}
            id={"nav-#{item.key}"}
            {link_attrs(@mode, item)}
            class={[
              "flex w-full items-center gap-2 rounded-md px-2 py-[3px] text-left text-[13px] transition-colors",
              if(@active == item.key,
                do: "bg-steer-soft text-steer-ink ring-1 ring-steer-ring ring-inset",
                else: "hover:bg-sunken"
              )
            ]}
          >
            <.icon
              name={item.icon}
              class={[
                "size-[15px] shrink-0",
                if(@active == item.key, do: "text-steer", else: "text-muted")
              ]}
            />
            <span class="truncate">{item.label}</span>
            <span :if={item.tone} class={["ml-auto size-1.5 rounded-full", dot(item.tone)]} />
            <span
              :if={item.badge}
              class={[
                "ml-auto font-mono text-[11px]",
                if(item.alert, do: "text-bad", else: "text-faint")
              ]}
            >
              {item.badge}
            </span>
          </.link>
        </div>
      </div>
    </div>
    """
  end

  defp link_attrs(:patch, %{patchable: true, to: to}), do: %{patch: to}
  defp link_attrs(_mode, %{to: to}), do: %{navigate: to}

  def groups(facts, pops) do
    sections(facts) ++ [drilldown(pops)]
  end

  defp drilldown(pops) do
    %{
      label: "Drilldown",
      icon: "hero-chart-bar-square",
      items:
        Enum.map(pops, fn pop ->
          %{
            key: "pop-#{pop.id}",
            label: pop.name || pop.id,
            icon: "hero-server",
            to: "/pops/#{pop.id}",
            patchable: false,
            badge: nil,
            alert: false,
            tone: node_tone(pop.status)
          }
        end)
    }
  end

  defp sections(facts) do
    [
      %{
        label: "Fabric",
        icon: "hero-share",
        items: [item(:links, "Links", "hero-link", count(facts, :links))]
      },
      %{
        label: "Routing",
        icon: "hero-arrows-right-left",
        items: [
          item(
            :paths,
            "Segment lists",
            "hero-queue-list",
            count(facts, :paths),
            wrong(facts, :paths)
          ),
          item(:computed, "Computed paths", "hero-cpu-chip", count(facts, :computed)),
          item(:registry, "SID allocations", "hero-hashtag", nil)
        ]
      },
      %{
        label: "Tenants",
        icon: "hero-building-office-2",
        items: [item(:sites, "Sites", "hero-map-pin", count(facts, :sites), wrong(facts, :sites))]
      },
      %{
        label: "Operations",
        icon: "hero-bolt",
        items: [item(:changes, "Changes", "hero-arrow-path", count(facts, :changes))]
      }
    ]
  end

  defp count(nil, _key), do: nil
  defp count(facts, key), do: Map.get(facts, key)

  defp wrong(nil, _key), do: 0
  defp wrong(facts, :paths), do: facts.paths_degraded
  defp wrong(facts, :sites), do: facts.sites - facts.sites_up

  # a section's badge is its size, unless something in it is wrong: then it is
  # how many things are wrong, in the bad tone
  defp item(key, label, icon, count, wrong \\ 0) do
    %{
      key: key,
      label: label,
      icon: icon,
      to: "/?section=#{key}",
      patchable: true,
      badge: if(wrong > 0, do: wrong, else: count),
      alert: wrong > 0,
      tone: nil
    }
  end
end
