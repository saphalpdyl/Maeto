.PHONY: lint lint.editorconfig generate deploy

TOPOLOGY_YAML := deploy/topologies/eight-pop.yaml
TOPOLOGY_NAME := eight-pop

setup:
	./scripts/hooks/install-hooks.sh

# generate containerlab + frr config from the topology dsl into build/<hash>
generate:
	. deploy/.venv/bin/activate
	PYTHONPATH=deploy python3 -m generator $(TOPOLOGY_YAML)

# deploy the topology recorded in .state/latest.json (build/<hash>/topology.yml)
deploy:
	@if [ ! -f .state/latest.json ]; then echo "no .state/latest.json; run 'make generate' first" >&2; exit 1; fi; \
	out=$$(python3 -c "import json; print(json.load(open('.state/latest.json'))['output'])"); \
	topo="$$out/topology.yml"; \
	if [ ! -f "$$topo" ]; then echo "missing $$topo; run 'make generate' first" >&2; exit 1; fi; \
	sudo clab deploy -t "$$topo" --reconfigure

lint:
	golangci-lint run ./...

# Enforce .editorconfig across the whole repo. Config: .editorconfig-checker.json.
lint.editorconfig:
	docker run --rm -v $(PWD):/check -w /check mstruebing/editorconfig-checker:latest ec

# Personal
sync:
	rsync -rav . maeto:/home/vagrant/maeto/ --exclude-from=.rsyncignore

## VM-related
build-vm:
	docker build -t maeto-host:latest -f docker/maeto-host.Dockerfile .

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
