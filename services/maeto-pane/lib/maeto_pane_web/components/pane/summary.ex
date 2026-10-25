defmodule MaetoPaneWeb.Pane.Summary do
  @moduledoc """
  The strip across the top: one cell per layer of the mesh, each a count of
  what is healthy plus a count of whatever is not.
  """

  use MaetoPaneWeb, :html

  import MaetoPaneWeb.Pane.UI

  attr :facts, :map, required: true
  attr :issues, :list, required: true

  def summary(assigns) do
    ~H"""
    <section
      id="summary"
      class="grid grid-cols-2 gap-y-4 border-b border-line px-6 py-5 xl:grid-cols-4"
    >
      <.cell label="PoPs">
        <.stat tone={if @facts.pops_ok == @facts.pops, do: "ok", else: "faint"}>
          {@facts.pops_ok}/{@facts.pops} reconciled
        </.stat>
        <.stat :if={@facts.pops_converging > 0} tone="warn">
          {@facts.pops_converging} converging
        </.stat>
        <.stat :if={@facts.pops_failing > 0} tone="bad">{@facts.pops_failing} failing</.stat>
      </.cell>

      <.cell label="Adjacencies">
        <.stat tone={if @facts.adjacencies_up == @facts.adjacencies, do: "ok", else: "bad"}>
          {@facts.adjacencies_up}/{@facts.adjacencies} up
        </.stat>
      </.cell>

      <.cell label="Segment lists in FIB">
        <.stat tone={if @facts.paths_installed == @facts.paths, do: "ok", else: "faint"}>
          {@facts.paths_installed}/{@facts.paths}
        </.stat>
        <.stat :if={@facts.paths_pending > 0} tone="warn">{@facts.paths_pending} pending</.stat>
        <.stat :if={@facts.paths_degraded > 0} tone="bad">{@facts.paths_degraded} drifted</.stat>
      </.cell>

      <.cell label="Sites reporting">
        <.stat tone={if @facts.sites_up == @facts.sites, do: "ok", else: "bad"}>
          {@facts.sites_up}/{@facts.sites} up
        </.stat>
        <.stat :if={@issues != []} tone="bad">{length(@issues)} issues</.stat>
      </.cell>
    </section>
    """
  end

  attr :label, :string, required: true
  slot :inner_block, required: true

  defp cell(assigns) do
    ~H"""
    <div class="flex flex-col gap-2">
      <div class="text-[13px] text-muted">{@label}</div>
      <div class="flex flex-wrap gap-1.5">{render_slot(@inner_block)}</div>
    </div>
    """
  end
end
