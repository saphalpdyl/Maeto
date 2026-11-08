# SPDX-License-Identifier: GPL-2.0-or-later
#
# Out-of-tree FRR modules for Maeto.
#
# Modules must be built against the exact FRR they are loaded into, so they
# compile against a staged install of the FRR fork in third_party/maeto-frr.

FRR_BUILD ?= ../third_party/maeto-frr/build
FRR_STAGE ?= $(CURDIR)/.frr-stage

SYS_PATH := /usr/bin:/bin:$(PATH)

PKG_CONFIG_ENV := PKG_CONFIG_PATH=$(FRR_STAGE)/usr/local/lib/pkgconfig \
		  PKG_CONFIG_SYSROOT_DIR=$(FRR_STAGE)

FRR_CFLAGS = $(shell $(PKG_CONFIG_ENV) pkg-config --cflags frr)

CFLAGS ?= -O2 -g
CFLAGS += -fPIC -Wall -Wextra -Wstrict-prototypes -Wmissing-prototypes
LDFLAGS += -shared

MODULES := bfdd_maeto.so

# Formatted with FRR's own .clang-format (copied from third_party/maeto-frr).
FORMAT_SRCS := $(wildcard *.c *.h)

.PHONY: all stage compdb format format-check clean check-stage

all: $(MODULES)

bfdd_maeto.so: maeto_bfd_notif_sender.c | check-stage
	$(CC) $(FRR_CFLAGS) $(CFLAGS) $(LDFLAGS) -o $@ $<

check-stage:
	@test -f $(FRR_STAGE)/usr/local/lib/pkgconfig/frr.pc || \
		{ echo "No staged FRR in $(FRR_STAGE); run 'make stage' first." >&2; exit 1; }

#   make stage   stage-install the FRR fork into $(FRR_STAGE)
stage:
	PATH=$(SYS_PATH) $(MAKE) -C $(FRR_BUILD) install DESTDIR=$(FRR_STAGE)

compdb:
	bear -- $(MAKE) -B all

format:
	clang-format -i $(FORMAT_SRCS)

format-check:
	clang-format --dry-run --Werror $(FORMAT_SRCS)

clean:
	rm -f $(MODULES)
