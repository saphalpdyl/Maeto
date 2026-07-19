.PHONY: lint lint.editorconfig

setup:
	./scripts/hooks/install-hooks.sh

lint:
	golangci-lint run ./...

# Enforce .editorconfig across the whole repo. Config: .editorconfig-checker.json.
lint.editorconfig:
	docker run --rm -v $(PWD):/check -w /check mstruebing/editorconfig-checker:latest ec

# Personal
sync:
	rsync -rav . cepheus:/home/vagrant/cepheus/ --exclude-from=.rsyncignore

## VM-related
clean:
	-sudo containerlab destroy -t clab/example-toplogy.yml
	-sudo docker rm -f $$(docker ps -aq --filter "name=^clab-retail-")

ips:
	@printf "%-24s %-60s\n" "Name" "Interfaces"
	@for c in $$(docker ps --format '{{.Names}}' | grep '^clab-retail-'); do \
	  ifaces=$$(docker exec -it "$$c" sh -c "ip -4 -o addr show scope global | awk '\$$2!=\"lo\" && \$$2!=\"eth0\" {print \$$2 \":\" \$$4}'" 2>/dev/null | tr -d '\r' | paste -sd ', ' -); \
	  printf "%-24s %-60s\n" "$$c" "$${ifaces:-<none>}"; \
	done

## Generators
sqlc-gen:
	UID=$(shell id -u) GID=$(shell id -g) docker compose -f docker-compose.dev.yml run --rm sqlc
