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

COPY services/maeto-portal/ ./services/maeto-portal/
COPY libs/ ./libs/

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    if [ "$DEBUG" = "1" ]; then \
      go install github.com/go-delve/delve/cmd/dlv@latest && cp /go/bin/dlv /out/ && \
      CGO_ENABLED=0 go build -gcflags="all=-N -l" -o /bin/maeto-portal ./services/maeto-portal/cmd; \
    else \
      CGO_ENABLED=0 go build -o /bin/maeto-portal ./services/maeto-portal/cmd; \
    fi

# FRR built from the fork (third_party/maeto-frr), which carries the bfdd YANG
# notifications. Everything is installed into /frr-root. Built on the same
# alpine as the runtime below so the shared libraries match.
FROM alpine:3.20 AS frr-build
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

# alpine 3.20 pins strongswan to 5.9.13, matching the frr-based pop image
FROM alpine:3.20

RUN apk add --no-cache \
      strongswan \
      iproute2 \
      supervisor \
      tcpdump \
      iputils \
      netcat-openbsd \
      curl \
      mandoc man-pages \
      bash json-c c-ares readline libcap pcre2 protobuf-c

RUN addgroup -S frr && addgroup -S frrvty && \
    adduser -S -D -h /var/run/frr -s /sbin/nologin -G frr -g frr frr && \
    adduser frr frrvty

COPY --from=frr-runtime /frr-root/ /
COPY --from=frr-modules-build /src/frr-modules/bfdd_maeto.so /usr/lib/frr/modules/bfdd_maeto.so
RUN mkdir -p /var/run/frr /var/log/frr && chown -R frr:frr /etc/frr /var/run/frr /var/log/frr
RUN install -d -m 0750 -o root -g frr /run/maeto

RUN mkdir -p /var/log/supervisor
COPY docker/conf/cpe/supervisord.conf /etc/supervisor/conf.d/supervisord.conf

RUN printf 'include conf.d/*.conf\n' > /etc/swanctl/swanctl.conf
COPY docker/conf/shared/charon-maeto.conf /etc/strongswan.d/maeto.conf

COPY --from=build /bin/maeto-portal /usr/local/bin/maeto-portal
COPY --from=build /out/ /usr/local/bin/
COPY docker/scripts/maeto-launch.sh /usr/local/bin/maeto-launch

WORKDIR /app

CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
