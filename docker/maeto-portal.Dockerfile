ARG GO_VERSION=1.25.12

FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY services/maeto-portal/ ./services/maeto-portal/
COPY libs/ ./libs/

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o /bin/maeto-portal ./services/maeto-portal/cmd

# alpine 3.20 pins strongswan to 5.9.13, matching the frr-based pop image
FROM alpine:3.20

RUN apk add --no-cache \
      strongswan \
      iproute2 \
      supervisor \
      tcpdump \
      iputils \
      netcat-openbsd \
      curl

RUN mkdir -p /var/log/supervisor
COPY docker/conf/cpe/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

RUN printf 'include conf.d/*.conf\n' > /etc/swanctl/swanctl.conf
COPY docker/conf/shared/charon-maeto.conf /etc/strongswan.d/maeto.conf

COPY --from=build /bin/maeto-portal /usr/local/bin/maeto-portal

WORKDIR /app

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
