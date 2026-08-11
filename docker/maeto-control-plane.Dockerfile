ARG GO_VERSION=1.25.12

FROM golang:${GO_VERSION}-alpine AS build

RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

COPY services/control-plane/ ./services/control-plane/
COPY libs/common/ ./libs/common/
COPY libs/telemetry/ ./libs/telemetry/

RUN CGO_ENABLED=0 go build -o /bin/maeto-control-plane ./services/control-plane/cmd

FROM alpine:3.21

COPY --from=build /bin/maeto-control-plane /usr/local/bin/maeto-control-plane

WORKDIR /app

ENTRYPOINT [ "maeto-control-plane" ]
