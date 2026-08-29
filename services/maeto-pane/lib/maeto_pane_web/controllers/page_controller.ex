defmodule MaetoPaneWeb.PageController do
  use MaetoPaneWeb, :controller

  def home(conn, _params) do
    render(conn, :home)
  end
end
