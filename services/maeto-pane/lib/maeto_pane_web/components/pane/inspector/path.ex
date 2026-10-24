defmodule MaetoPaneWeb.Pane.Inspector.PathPanel do
  @moduledoc """
  A selected segment list: the decision, its hops, and desired against the observed FIB.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.Tones
  import MaetoPaneWeb.Pane.UI

  attr :selected, :map, required: true

  def path_panel(assigns) do
    ~H"""
    <header class="flex items-start gap-2.5 border-b border-line px-4 py-3">
      <div class="min-w-0 flex-1">
        <h2 class="truncate text-sm font-semibold">
          {@selected.pe_name || @selected.pe} &rarr; {@selected.dest_name || @selected.dest || "?"}
        </h2>
        <p class="mt-0.5 truncate text-xs text-muted">
          {Enum.join(
            Enum.reject(
              [@selected.site && @selected.site.cpe, @selected.tenant, @selected.class],
              &is_nil/1
            ),
            " · "
          )}
        </p>
      </div>
      <.esc />
    </header>

    <dl class="grid grid-cols-3 gap-3 border-b border-line px-4 py-3.5">
      <div class="col-span-3 space-y-1">
        <dt class="text-xs text-faint">Destination</dt>
        <dd class="truncate font-mono text-[13px]">{@selected.prefix}</dd>
      </div>
      <div class="space-y-1">
        <dt class="text-xs text-faint">Path cost</dt>
        <dd class="font-mono text-[13px]">{num(@selected.cost)}</dd>
      </div>
      <div class="space-y-1">
        <dt class="text-xs text-faint">Hops</dt>
        <dd class="font-mono text-[13px]">{length(@selected.hops)}</dd>
      </div>
      <div class="space-y-1">
        <dt class="text-xs text-faint">Table</dt>
        <dd class="truncate font-mono text-[13px]">{@selected.table || "-"}</dd>
      </div>
    </dl>

    <section class="border-b border-line px-4 py-3.5">
      <h3 class="mb-3 text-[13px] font-medium">Desired path</h3>
      <ol>
        <.step
          tag={@selected.pe}
          name={@selected.pe_name || @selected.pe}
          role="ingress"
          detail="H.Encaps"
          last={@selected.hops == []}
        />
        <.step
          :for={{hop, index} <- Enum.with_index(@selected.hops, 1)}
          tag={hop.node || "?"}
          name={hop.node_name || "unresolved"}
          role={if index == length(@selected.hops), do: "egress"}
          detail={"#{hop.sid} #{String.upcase(hop_behavior(hop.role))}"}
          last={index == length(@selected.hops)}
        />
      </ol>
    </section>

    <section class="space-y-2 px-4 py-3.5">
      <div class="flex items-center justify-between gap-2">
        <h3 class="text-[13px] font-medium">Observed FIB</h3>
        <.pill tone={path_tone(@selected.status)}>{path_label(@selected.status)}</.pill>
      </div>
      <div
        :if={@selected.status != :installed}
        class={["font-mono text-xs break-all", @selected.status == :drifted && "text-bad"]}
      >
        {if @selected.observed, do: Enum.join(@selected.observed, " › "), else: "no route"}
      </div>
    </section>
    """
  end

  attr :tag, :string, required: true
  attr :name, :string, required: true
  attr :role, :string, default: nil
  attr :detail, :string, required: true
  attr :last, :boolean, default: false

  defp step(assigns) do
    ~H"""
    <li class="grid grid-cols-[22px_1fr] gap-x-2.5">
      <div class="flex flex-col items-center">
        <span class="flex size-[22px] shrink-0 items-center justify-center rounded-md border border-steer bg-surface text-[11px] font-semibold">
          {@tag}
        </span>
        <span :if={!@last} class="min-h-3 w-px flex-1 bg-line" />
      </div>
      <div class={["min-w-0", !@last && "pb-3"]}>
        <div class="text-[13px] leading-[22px]">
          {@name}<span :if={@role} class="text-muted"> · {@role}</span>
        </div>
        <div class="truncate font-mono text-[11px] text-muted">{@detail}</div>
      </div>
    </li>
    """
  end
end
