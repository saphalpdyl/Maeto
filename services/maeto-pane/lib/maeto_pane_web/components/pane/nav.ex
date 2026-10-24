defmodule MaetoPaneWeb.Pane.Nav do
  @moduledoc """
  The sidebar: the pane's sections grouped by the layer they describe, each with
  a count and, when something is wrong in it, a count in the bad tone.
  """

  use MaetoPaneWeb, :html

  attr :groups, :list, required: true
  attr :section, :atom, required: true

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
          <button
            :for={item <- group.items}
            id={"nav-#{item.key}"}
            phx-click="section"
            phx-value-section={item.key}
            class={[
              "flex w-full items-center gap-2 rounded-md px-2 py-[3px] text-left text-[13px] transition-colors",
              if(@section == item.key,
                do: "bg-steer-soft text-steer-ink ring-1 ring-steer-ring ring-inset",
                else: "hover:bg-sunken"
              )
            ]}
          >
            <.icon
              name={item.icon}
              class={[
                "size-[15px] shrink-0",
                if(@section == item.key, do: "text-steer", else: "text-muted")
              ]}
            />
            <span class="truncate">{item.label}</span>
            <span
              :if={item.badge}
              class={[
                "ml-auto font-mono text-[11px]",
                if(item.alert, do: "text-bad", else: "text-faint")
              ]}
            >
              {item.badge}
            </span>
          </button>
        </div>
      </div>
    </div>
    """
  end
end
