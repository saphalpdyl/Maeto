ARG GO_VERSION=1.25.12

FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

COPY services/maeto-agent/ ./services/maeto-agent/
COPY libs/common/ ./libs/common/
COPY libs/telemetry/ ./libs/telemetry/

RUN CGO_ENABLED=0 go build -o /bin/maeto-agent ./services/maeto-agent/cmd

# PoP node: FRR for IS-IS + SRv6, strongSwan for customer IPsec termination.
FROM quay.io/frrouting/frr:10.4.1

RUN apk add --no-cache strongswan nftables supervisor

RUN mkdir -p /var/log/supervisor
COPY docker/conf/pop/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

RUN printf 'include conf.d/*.conf\n' > /etc/swanctl/swanctl.conf

COPY docker/conf/shared/charon-maeto.conf /etc/strongswan.d/maeto.conf

COPY --from=build /bin/maeto-agent /usr/local/bin/maeto-agent

# WARN: frr daemon start '/usr/lib/frr/docker-start' happens in topology.yaml > exec:

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
