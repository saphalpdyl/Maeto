.PHONY: lint lint.editorconfig generate deploy install-virtual-environments fe ap ddbg dev ips

TOPOLOGY_YAML := clab/topologies/eight-pop.yaml
TOPOLOGY_NAME := eight-pop
STATE_FILE := .state/latest.json

CLAB_PY := clab/.venv/bin/python
CGOVER_DIR := tools/debug/control-plane
CGOVER_PY := $(CGOVER_DIR)/.venv/bin/python

# DEBUG=1 delve attached binary; for debugging purposes
DEBUG ?= 0
ifeq ($(DEBUG),1)
export MAETO_DEBUG := 1
export MAETO_DLV_LISTEN := [::]:2345
endif

setup: install-virtual-environments
	./scripts/hooks/install-hooks.sh

install-virtual-environments:
	python3 -m venv --clear clab/.venv
	$(CLAB_PY) -m pip install -q --upgrade pip
	$(CLAB_PY) -m pip install -q -r clab/generator/requirements.txt
	python3 -m venv --clear $(CGOVER_DIR)/.venv
	$(CGOVER_PY) -m pip install -q --upgrade pip
	$(CGOVER_PY) -m pip install -q -r $(CGOVER_DIR)/requirements.txt

# generate containerlab + frr config from the topology dsl into build/<hash>
generate:
	./scripts/clab/generate.sh $(CLAB_PY) $(TOPOLOGY_YAML)

# deploy the topology recorded in $(STATE_FILE) (build/<hash>/topology.yml)
deploy:
	./scripts/clab/deploy.sh $(STATE_FILE)

lint:
	golangci-lint run ./...

# Enforce .editorconfig across the whole repo. Config: .editorconfig-checker.json.
lint.editorconfig:
	docker run --rm -v $(PWD):/check -w /check mstruebing/editorconfig-checker:latest editorconfig-checker

# Personal
sync:
	rsync -rav . maeto:/home/vagrant/maeto/ --exclude-from=.rsyncignore

## VM-related

# The services including NATS, DB etc. will run inside the VM as to
# not fragment deployment during development and makes things simpler
dev:
	DEBUG=$(DEBUG) docker compose up --build maeto-control-plane

fe:
	docker compose up --build maeto-pane

ddbg: # dev debug
	DEBUG=1 docker compose up --build maeto-control-plane

build-vm:
	docker build --build-arg DEBUG=$(DEBUG) -t maeto-control-plane:latest -f docker/maeto-control-plane.Dockerfile .
	docker build --build-arg DEBUG=$(DEBUG) -t maeto-pop:latest -f docker/maeto-pop.Dockerfile .
	docker build --build-arg DEBUG=$(DEBUG) -t maeto-portal:latest -f docker/maeto-portal.Dockerfile .

clean:
	./scripts/clab/clean.sh $(TOPOLOGY_NAME) $(STATE_FILE)

ips:
	@./scripts/clab/ips.sh $(TOPOLOGY_NAME)

# Dozzle Container logs viewer
dozzle:
	docker run -d -v /var/run/docker.sock:/var/run/docker.sock -v dozzle_data:/data -p 8080:8080 amir20/dozzle:latest

# NATS NUI
nui:
	docker compose -f docker-compose.dev.yml up nats-nui

apply: clean build-vm generate deploy
	$(MAKE) ips

ap:
	-make clean
	rm -rf build/
	$(MAKE) generate
	$(MAKE) apply

apd:
	-make clean
	rm -rf build/
	$(MAKE) generate
	$(MAKE) DEBUG=1 apply

## Generators
sqlc-gen:
	UID=$(shell id -u) GID=$(shell id -g) docker compose -f docker-compose.dev.yml run --rm sqlc

PROTO_RUN = docker run --rm -v $(PWD):/workspace -w /workspace/libs/proto

proto-image:
	docker build -t maeto-buf -f docker/gen/protobuf.gen.Dockerfile docker/gen

proto-gen: proto-image
	$(PROTO_RUN) -u $(shell id -u):$(shell id -g) -e HOME=/tmp maeto-buf generate

proto-lint: proto-image
	$(PROTO_RUN) maeto-buf lint

# !VM ONLY!
# One-time CA cert generation used by generator to generate PoP and CPE certs
pki:
	mkdir -p .certs/
	pki --gen --type ecdsa --size 256 --outform pem > .certs/ca-key.pem
	pki --self --ca --lifetime 7300 --in .certs/ca-key.pem --type priv --dn "CN=maeto-ca" --outform pem > .certs/ca-cert.pem

## Testing
test.build:
	docker build -t test-stamp-suite:latest -f docker/tests/stamp.test.Dockerfile .

test.unit:
	go test -v ./libs/stamp/...
	go test -v ./libs/probe/...
	go test -v ./services/...

test.integration: test.build
	docker compose -f docker-compose.test.yaml down
	docker compose -f docker-compose.test.yaml up -d
	go test -v -tags integration ./libs/stamp/tests/integration
	go test -v -tags integration ./libs/probe/tests/integration
	docker compose -f docker-compose.test.yaml down
