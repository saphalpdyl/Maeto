defmodule MaetoPaneWeb.Pane.Reconciliation do
  @moduledoc """
  The mesh's reconciliation state at a glance: one overall verdict and the four
  layers it is made of, each as healthy-over-total.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.UI

  attr :facts, :map, required: true

  def reconciliation(assigns) do
    assigns = assign(assigns, :verdict, verdict(assigns.facts))

    ~H"""
    <section id="reconciliation" class="flex h-full flex-col">
      <div class="flex items-center gap-2.5 px-5 py-4">
        <h2 class="text-[15px] font-medium">Reconciliation</h2>
        <.stat tone={elem(@verdict, 0)}>{elem(@verdict, 1)}</.stat>
      </div>

      <div class="grid flex-1 grid-cols-2 border-t border-line">
        <.cell label="PoPs" class="border-r border-b">
          <.stat tone={if @facts.pops_ok == @facts.pops, do: "ok", else: "warn"}>
            {@facts.pops_ok} / {@facts.pops}
          </.stat>
        </.cell>
        <.cell label="Adjacencies" class="border-b">
          <.stat tone={if @facts.adjacencies_up == @facts.adjacencies, do: "ok", else: "bad"}>
            {@facts.adjacencies_up} / {@facts.adjacencies}
          </.stat>
        </.cell>
        <.cell label="Segment lists in FIB" class="border-r">
          <.stat tone={paths_tone(@facts)}>{@facts.paths_installed} / {@facts.paths}</.stat>
        </.cell>
        <.cell label="Sites reporting">
          <.stat tone={if @facts.sites_up == @facts.sites, do: "ok", else: "bad"}>
            {@facts.sites_up} / {@facts.sites}
          </.stat>
        </.cell>
      </div>
    </section>
    """
  end

  attr :label, :string, required: true
  attr :class, :string, default: nil
  slot :inner_block, required: true

  defp cell(assigns) do
    ~H"""
    <div class={["flex flex-col gap-2 border-line px-5 py-4", @class]}>
      <div class="text-[13px] text-muted">{@label}</div>
      <div>{render_slot(@inner_block)}</div>
    </div>
    """
  end

  defp paths_tone(%{paths_degraded: degraded}) when degraded > 0, do: "bad"
  defp paths_tone(%{paths_installed: same, paths: same}), do: "ok"
  defp paths_tone(_facts), do: "warn"

  defp verdict(facts) do
    cond do
      facts.pops_failing > 0 or facts.paths_degraded > 0 -> {"bad", "Issue"}
      facts.pops_converging > 0 or facts.paths_pending > 0 -> {"warn", "Converging"}
      true -> {"ok", "Reconciled"}
    end
  end
end
