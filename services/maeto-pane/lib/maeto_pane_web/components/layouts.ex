defmodule MaetoPaneWeb.Layouts do
  @moduledoc """
  This module holds layouts and related functionality
  used by your application.
  """
  use MaetoPaneWeb, :html

  # Embed all files in layouts/* within this module.
  # The default root.html.heex file contains the HTML
  # skeleton of your application, namely HTML headers
  # and other static content.
  embed_templates "layouts/*"

  @doc """
  Renders your app layout.

  This function is typically invoked from every template,
  and it often contains your application menu, sidebar,
  or similar.

  ## Examples

      <Layouts.app flash={@flash}>
        <h1>Content</h1>
      </Layouts.app>

  """
  attr :flash, :map, required: true, doc: "the map of flash messages"

  attr :current_scope, :map,
    default: nil,
    doc: "the current [scope](https://hexdocs.pm/phoenix/scopes.html)"

  slot :rail, doc: "section links down the sidebar"
  slot :crumbs, doc: "context that sits inline with the brand in the top bar"
  slot :status, doc: "liveness indicators pinned to the right of the top bar"
  slot :inner_block, required: true

  def app(assigns) do
    ~H"""
    <div class="flex min-h-dvh bg-canvas text-ink">
      <aside class="sticky top-0 hidden h-dvh w-[212px] shrink-0 flex-col border-r border-line bg-rail lg:flex">
        <a href="/" class="flex h-12 shrink-0 items-center gap-2 px-4">
          <.mark />
          <span class="text-[14px] font-semibold tracking-tight">
            ma<span class="text-accent">e</span>to
          </span>
        </a>

        <nav :if={@rail != []} class="mt-scroll flex-1 space-y-0.5 overflow-y-auto px-2 py-2">
          {render_slot(@rail)}
        </nav>

        <div class="shrink-0 border-t border-line p-2">
          <.theme_toggle />
        </div>
      </aside>

      <div class="flex min-w-0 flex-1 flex-col">
        <header class="sticky top-0 z-20 flex h-12 shrink-0 items-center gap-3 border-b border-line bg-canvas/95 px-4 backdrop-blur sm:px-6">
          <a href="/" class="flex shrink-0 items-center gap-2 lg:hidden">
            <.mark />
            <span class="text-[14px] font-semibold">ma<span class="text-accent">e</span>to</span>
          </a>

          <div class="mt-scroll flex min-w-0 items-center gap-2 overflow-x-auto">
            {render_slot(@crumbs)}
          </div>

          <div class="ml-auto flex shrink-0 items-center gap-2">
            {render_slot(@status)}
          </div>
        </header>

        <main class="min-w-0 flex-1 px-4 py-6 sm:px-6 lg:px-8">
          <div class="mx-auto max-w-[1360px]">
            {render_slot(@inner_block)}
          </div>
        </main>
      </div>
    </div>

    <.flash_group flash={@flash} />
    """
  end

  @doc "The maeto leaf mark, tinted from the active theme."
  def mark(assigns) do
    ~H"""
    <svg viewBox="0 0 54 64" class="h-[18px] w-[15px] shrink-0" aria-hidden="true">
      <path d="M6,60 C6,30 20,4 40,4 C34,20 34,42 6,60 Z" class="fill-muted" />
      <path d="M18,60 C18,34 30,10 48,10 C44,26 42,46 18,60 Z" class="fill-accent" />
    </svg>
    """
  end

  @doc """
  Shows the flash group with standard titles and content.

  ## Examples

      <.flash_group flash={@flash} />
  """
  attr :flash, :map, required: true, doc: "the map of flash messages"
  attr :id, :string, default: "flash-group", doc: "the optional id of flash container"

  def flash_group(assigns) do
    ~H"""
    <div id={@id} aria-live="polite">
      <.flash kind={:info} flash={@flash} />
      <.flash kind={:error} flash={@flash} />

      <.flash
        id="client-error"
        kind={:error}
        title={gettext("We can't find the internet")}
        phx-disconnected={show(".phx-client-error #client-error") |> JS.remove_attribute("hidden")}
        phx-connected={hide("#client-error") |> JS.set_attribute({"hidden", ""})}
        hidden
      >
        {gettext("Attempting to reconnect")}
        <.icon name="hero-arrow-path" class="ml-1 size-3 motion-safe:animate-spin" />
      </.flash>

      <.flash
        id="server-error"
        kind={:error}
        title={gettext("Something went wrong!")}
        phx-disconnected={show(".phx-server-error #server-error") |> JS.remove_attribute("hidden")}
        phx-connected={hide("#server-error") |> JS.set_attribute({"hidden", ""})}
        hidden
      >
        {gettext("Attempting to reconnect")}
        <.icon name="hero-arrow-path" class="ml-1 size-3 motion-safe:animate-spin" />
      </.flash>
    </div>
    """
  end

  @doc """
  Provides dark vs light theme toggle based on themes defined in app.css.

  See <head> in root.html.heex which applies the theme before page load.
  """
  def theme_toggle(assigns) do
    ~H"""
    <div class="relative flex flex-row items-center rounded-md border border-line bg-canvas">
      <div class="absolute left-0 h-full w-1/3 rounded-md border border-line-strong bg-surface [[data-theme=dark]_&]:left-2/3 [[data-theme=light]_&]:left-1/3 transition-[left]" />

      <button
        class="flex w-1/3 cursor-pointer justify-center p-1.5"
        phx-click={JS.dispatch("phx:set-theme")}
        data-phx-theme="system"
      >
        <.icon
          name="hero-computer-desktop-micro"
          class="relative size-3.5 opacity-70 hover:opacity-100"
        />
      </button>

      <button
        class="flex w-1/3 cursor-pointer justify-center p-1.5"
        phx-click={JS.dispatch("phx:set-theme")}
        data-phx-theme="light"
      >
        <.icon name="hero-sun-micro" class="relative size-3.5 opacity-70 hover:opacity-100" />
      </button>

      <button
        class="flex w-1/3 cursor-pointer justify-center p-1.5"
        phx-click={JS.dispatch("phx:set-theme")}
        data-phx-theme="dark"
      >
        <.icon name="hero-moon-micro" class="relative size-3.5 opacity-70 hover:opacity-100" />
      </button>
    </div>
    """
  end
end
