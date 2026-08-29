.PHONY: lint lint.editorconfig generate deploy

TOPOLOGY_YAML := clab/topologies/eight-pop.yaml
TOPOLOGY_NAME := eight-pop

# DEBUG=1 builds unoptimised images that ship delve, and makes the generator
# put MAETO_DLV_LISTEN in every node's env. See docker/scripts/maeto-launch.sh
DEBUG ?= 0
ifeq ($(DEBUG),1)
export MAETO_DEBUG := 1
export MAETO_DLV_LISTEN := [::]:2345
endif

setup:
	./scripts/hooks/install-hooks.sh

# generate containerlab + frr config from the topology dsl into build/<hash>
generate:
	. clab/.venv/bin/activate
	PYTHONPATH=clab python3 -m generator $(TOPOLOGY_YAML)

# deploy the topology recorded in .state/latest.json (build/<hash>/topology.yml)
deploy:
	@if [ ! -f .state/latest.json ]; then echo "no .state/latest.json; run 'make generate' first" >&2; exit 1; fi; \
	out=$$(python3 -c "import json; print(json.load(open('.state/latest.json'))['output'])"); \
	topo="$$out/topology.yml"; \
	if [ ! -f "$$topo" ]; then echo "missing $$topo; run 'make generate' first" >&2; exit 1; fi; \
	sudo modprobe vrf; \
	sudo clab deploy -t "$$topo" --reconfigure; \
	sudo sh "$$out/mgmt_routes.sh"

lint:
	golangci-lint run ./...

# Enforce .editorconfig across the whole repo. Config: .editorconfig-checker.json.
lint.editorconfig:
	docker run --rm -v $(PWD):/check -w /check mstruebing/editorconfig-checker:latest ec

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
	DEBUG=1 docker compsoe up --build maeto-control-plane

build-vm:
	docker build --build-arg DEBUG=$(DEBUG) -t maeto-pop:latest -f docker/maeto-pop.Dockerfile .
	docker build --build-arg DEBUG=$(DEBUG) -t maeto-portal:latest -f docker/maeto-portal.Dockerfile .

clean:
	@if [ ! -f .state/latest.json ]; then echo "no .state/latest.json; run 'make generate' first" >&2; exit 1; fi; \
	out=$$(python3 -c "import json; print(json.load(open('.state/latest.json'))['output'])"); \
	topo="$$out/topology.yml"; \
	if [ ! -f "$$topo" ]; then echo "missing $$topo; run 'make generate' first" >&2; exit 1; fi; \

	-sudo containerlab destroy -t "$$topo"
	-sudo docker rm -f $$(docker ps -aq --filter "name=^clab-$(TOPOLOGY_NAME)-")

ips:
	@printf "%-24s %s\n" "Name" "Interfaces"
	@for c in $$(docker ps --format '{{.Names}}' | grep '^clab-$(TOPOLOGY_NAME)-'); do \
		printf "%-24s\n" "$$c"; \
		docker exec "$$c" sh -c \
			"ip -6 -o addr show scope global | \
			 awk '\$$2 != \"eth0\" {printf \"  %-20s %s\n\", \$$2\":\", \$$4}'" \
			2>/dev/null || echo "  <none>"; \
		echo; \
	done

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

# One-time CA cert generation used by generator to generate PoP and CPE certs
pki:
	mkdir -p .certs/
	pki --gen --type ecdsa --size 256 --outform pem > .certs/ca-key.pem
	pki --self --ca --lifetime 7300 --in .certs/ca-key.pem --type priv --dn "CN=maeto-ca" --outform pem > .certs/ca-cert.pem
