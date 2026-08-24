ARG GO_VERSION=1.25.12
# DEBUG=1 builds unoptimised and ships delve; see docker/scripts/maeto-launch.sh
ARG DEBUG=0

FROM golang:${GO_VERSION}-alpine AS build

RUN apk add --no-cache gcc musl-dev
ARG DEBUG
WORKDIR /src
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY services/control-plane/ ./services/control-plane/
COPY libs/ ./libs/

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    if [ "$DEBUG" = "1" ]; then \
      go install github.com/go-delve/delve/cmd/dlv@latest && cp /go/bin/dlv /out/ && \
      CGO_ENABLED=0 go build -gcflags="all=-N -l" -o /bin/maeto-control-plane ./services/control-plane/cmd; \
    else \
      CGO_ENABLED=0 go build -o /bin/maeto-control-plane ./services/control-plane/cmd; \
    fi

FROM alpine:3.21

COPY --from=build /bin/maeto-control-plane /usr/local/bin/maeto-control-plane
COPY --from=build /out/ /usr/local/bin/
COPY docker/scripts/maeto-launch.sh /usr/local/bin/maeto-launch

WORKDIR /app

ENTRYPOINT [ "/usr/local/bin/maeto-launch", "/usr/local/bin/maeto-control-plane" ]
