ARG GO_VERSION=1.25.12

FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

COPY services/maeto-portal/ ./services/maeto-portal/
COPY libs/common/ ./libs/common/
COPY libs/telemetry/ ./libs/telemetry/

RUN CGO_ENABLED=0 go build -o /bin/maeto-portal ./services/maeto-portal/cmd

FROM nicolaka/netshoot:latest

COPY --from=build /bin/maeto-portal /usr/local/bin/maeto-portal

WORKDIR /app

ENTRYPOINT [ "maeto-portal" ]
