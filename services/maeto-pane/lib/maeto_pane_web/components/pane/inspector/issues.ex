defmodule MaetoPaneWeb.Pane.Inspector.Issues do
  @moduledoc """
  Nothing selected: the open invariant violations, if any.
  """

  use MaetoPaneWeb, :html

  attr :issues, :list, required: true

  def issues_panel(assigns) do
    ~H"""
    <header class="border-b border-line px-4 py-3">
      <h2 class="text-sm font-semibold">
        {if @issues == [], do: "Inspector", else: "Needs attention"}
      </h2>
      <p class="text-xs text-faint">
        {if @issues == [], do: "Nothing selected", else: "#{length(@issues)} items · nothing selected"}
      </p>
    </header>

    <p :if={@issues == []} class="px-4 py-4 text-[13px] text-muted">No open issues</p>

    <ul :if={@issues != []} class="mt-scroll max-h-[560px] overflow-auto">
      <li
        :for={issue <- @issues}
        class="space-y-1.5 border-b border-line/60 px-4 py-3.5 last:border-0"
      >
        <div class="flex items-center gap-2">
          <span class="size-[7px] shrink-0 rounded-full bg-bad" />
          <span class="font-mono text-[13px] font-medium">{issue.kind}</span>
          <span :if={issue.sid} class="ml-auto truncate font-mono text-[11px] text-faint">
            {issue.sid}
          </span>
        </div>
        <p class="text-[13px] text-pretty text-muted">{issue.detail}</p>
        <button
          :if={issue.node}
          phx-click="select"
          phx-value-kind="node"
          phx-value-id={issue.node}
          class="text-xs text-link hover:underline"
        >
          Inspect {issue.node}
        </button>
      </li>
    </ul>
    """
  end
end
