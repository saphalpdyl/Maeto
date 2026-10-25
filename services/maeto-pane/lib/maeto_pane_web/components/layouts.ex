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
  slot :foot, doc: "context pinned to the bottom of the sidebar"
  slot :inspector, doc: "a full-height column between the sidebar and the page"
  slot :inner_block, required: true

  def app(assigns) do
    ~H"""
    <div class="flex min-h-dvh bg-surface text-ink">
      <aside class="sticky top-0 hidden h-dvh w-[216px] shrink-0 flex-col border-r border-line bg-blue lg:flex">
        <a href="/" class="flex h-12 items-center px-3.5">
          <.logo class="h-10" />
        </a>

        <nav :if={@rail != []} class="mt-scroll flex-1 overflow-y-auto px-2.5 pb-3">
          {render_slot(@rail)}
        </nav>

        <div class="shrink-0 space-y-2 border-t border-line p-2.5">
          {render_slot(@foot)}
          <.theme_toggle />
        </div>
      </aside>

      <div class="flex min-w-0 flex-1 flex-col">
        <header class="sticky top-0 z-20 flex h-12 shrink-0 items-center gap-3 border-b border-line bg-surface/95 px-4 backdrop-blur sm:px-5">
          <a href="/" class="flex shrink-0 items-center lg:hidden">
            <.logo class="h-10" />
          </a>

          <div class="mt-scroll flex min-w-0 items-center gap-2 overflow-x-auto">
            {render_slot(@crumbs)}
          </div>

          <div class="ml-auto flex shrink-0 items-center gap-2">
            {render_slot(@status)}
          </div>
        </header>

        <div class="flex min-w-0 flex-1">
          <aside
            :if={@inspector != []}
            id="inspector"
            class="mt-scroll sticky top-12 hidden h-[calc(100dvh-3rem)] w-[30rem] shrink-0 overflow-y-auto border-r border-line bg-surface xl:block"
          >
            {render_slot(@inspector)}
          </aside>

          <main class="min-w-0 flex-1 bg-surface">
            {render_slot(@inner_block)}
          </main>
        </div>
      </div>
    </div>

    <.flash_group flash={@flash} />
    """
  end

  @doc "The maeto logo, swapped with the active theme."
  attr :class, :string, default: nil

  def logo(assigns) do
    ~H"""
    <img
      src={~p"/images/maeto-logo-dark.svg"}
      alt="maeto"
      class={["w-auto [[data-theme=light]_&]:hidden", @class]}
    />
    <img
      src={~p"/images/maeto-logo.svg"}
      alt="maeto"
      class={["hidden w-auto [[data-theme=light]_&]:block", @class]}
    />
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
