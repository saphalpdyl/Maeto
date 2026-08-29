defmodule MaetoPane.Repo do
  use Ecto.Repo,
    otp_app: :maeto_pane,
    adapter: Ecto.Adapters.Postgres
end
