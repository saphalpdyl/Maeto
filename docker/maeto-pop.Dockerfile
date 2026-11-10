ARG GO_VERSION=1.25.12
# DEBUG=1 builds unoptimised and ships delve; see docker/scripts/maeto-launch.sh
ARG DEBUG=0

FROM golang:${GO_VERSION}-alpine AS build

ARG DEBUG
WORKDIR /src
COPY go.mod go.sum* ./
COPY third_party/ ./third_party/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY services/maeto-agent/ ./services/maeto-agent/
COPY libs/ ./libs/

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    if [ "$DEBUG" = "1" ]; then \
      go install github.com/go-delve/delve/cmd/dlv@latest && cp /go/bin/dlv /out/ && \
      CGO_ENABLED=0 go build -gcflags="all=-N -l" -o /bin/maeto-agent ./services/maeto-agent/cmd; \
    else \
      CGO_ENABLED=0 go build -o /bin/maeto-agent ./services/maeto-agent/cmd; \
    fi

FROM alpine:3.22.3 AS xdp-bfd-build
RUN apk add --no-cache build-base clang20 llvm20 libbpf-dev linux-headers
WORKDIR /src
COPY third_party/xdp-bfd/ .
RUN make bfd_xdp.o bfd_tx

# FRR built from the fork (third_party/maeto-frr), which carries the bfdd YANG
# notifications. Everything is installed into /frr-root.
FROM alpine:3.22.3 AS frr-build
ARG LIBYANG_VERSION=2.1.128
RUN apk add --no-cache \
      build-base linux-headers bsd-compat-headers pkgconf curl \
      autoconf automake libtool bison flex gawk perl \
      cmake pcre2-dev \
      python3 python3-dev elfutils-dev \
      json-c-dev c-ares-dev readline-dev libcap-dev \
      protobuf-c-dev protobuf-c-compiler
WORKDIR /src

# libyang: the same version the upstream FRR images ship.
RUN curl -fsSL https://github.com/CESNET/libyang/archive/v${LIBYANG_VERSION}.tar.gz | tar xz && \
    cmake -S libyang-${LIBYANG_VERSION} -B libyang-build \
      -DCMAKE_BUILD_TYPE=Release \
      -DCMAKE_INSTALL_PREFIX=/usr \
      -DCMAKE_INSTALL_LIBDIR=lib \
      -DENABLE_TESTS=OFF && \
    make -C libyang-build -j"$(nproc)" && \
    make -C libyang-build install && \
    make -C libyang-build install DESTDIR=/frr-root

COPY third_party/maeto-frr/ ./frr/
RUN cd frr && \
    ./bootstrap.sh && \
    ./configure \
      --prefix=/usr \
      --sysconfdir=/etc \
      --localstatedir=/var \
      --sbindir=/usr/lib/frr \
      --libdir=/usr/lib \
      --enable-vtysh \
      --enable-multipath=64 \
      --enable-vty-group=frrvty \
      --enable-user=frr \
      --enable-group=frr \
      --enable-pcre2posix \
      --disable-doc && \
    make -j"$(nproc)" && \
    make install DESTDIR=/frr-root && \
    install -D -m 0644 tools/etc/frr/daemons /frr-root/etc/frr/daemons && \
    install -D -m 0755 docker/alpine/docker-start /frr-root/usr/lib/frr/docker-start

# Maeto's FRR modules, compiled against the FRR built above (no stable ABI).
FROM frr-build AS frr-modules-build
COPY frr-modules/ ./frr-modules/
RUN make -C frr-modules -B FRR_STAGE=/frr-root FRR_PREFIX=/usr

# Runtime copy of /frr-root without headers, pkg-config and libtool files.
FROM frr-build AS frr-runtime
RUN rm -rf /frr-root/usr/include /frr-root/usr/lib/pkgconfig /frr-root/usr/share/doc && \
    find /frr-root -name '*.la' -delete


# PoP node: FRR for IS-IS + SRv6, strongSwan for customer IPsec termination.
FROM alpine:3.22.3

RUN apk add --no-cache tini bash python3 iproute2 json-c c-ares readline libcap pcre2 protobuf-c && \
    apk add --no-cache strongswan nftables supervisor tcpdump mandoc man-pages libbpf

RUN addgroup -S frr && addgroup -S frrvty && \
    adduser -S -D -h /var/run/frr -s /sbin/nologin -G frr -g frr frr && \
    adduser frr frrvty

COPY --from=frr-runtime /frr-root/ /
COPY --from=frr-modules-build /src/frr-modules/bfdd_maeto.so /usr/lib/frr/modules/bfdd_maeto.so
RUN mkdir -p /var/run/frr && chown -R frr:frr /etc/frr /var/run/frr
RUN mkdir -p /var/run/frr/maeto/
RUN install -d -m 0750 -o root -g frr /run/maeto

RUN mkdir -p /var/log/supervisor
RUN mkdir -p /var/log/maeto
RUN mkdir -p /var/log/frr
COPY docker/conf/pop/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

RUN printf 'include conf.d/*.conf\n' > /etc/swanctl/swanctl.conf

COPY docker/conf/shared/charon-maeto.conf /etc/strongswan.d/maeto.conf

COPY --from=build /bin/maeto-agent /usr/local/bin/maeto-agent
COPY --from=build /out/ /usr/local/bin/
COPY --from=xdp-bfd-build /src/bfd_tx /usr/local/sbin/xdp-bfd
COPY --from=xdp-bfd-build /src/bfd_xdp.o /usr/local/sbin/bfd_xdp.o

COPY docker/scripts/maeto-launch.sh /usr/local/bin/maeto-launch
COPY docker/scripts/maeto-monitor.sh /usr/local/bin/maeto-monitor

# WARN: frr daemon start '/usr/lib/frr/docker-start' happens in topology.yaml > exec:

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
