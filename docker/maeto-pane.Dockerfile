ARG ELIXIR_VERSION=1.19.5
ARG OTP_VERSION=28.5
ARG DEBIAN_VERSION=trixie-20260421-slim

FROM hexpm/elixir:${ELIXIR_VERSION}-erlang-${OTP_VERSION}-debian-${DEBIAN_VERSION}

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        git \
        inotify-tools \
    && rm -rf /var/lib/apt/lists/*

RUN mix local.hex --force && mix local.rebar --force

ENV MIX_ENV=dev

WORKDIR /app

EXPOSE 4000

CMD ["sh", "-c", "mix deps.get && mix assets.setup && mix phx.server"]
