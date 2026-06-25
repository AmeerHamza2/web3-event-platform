.PHONY: build test vet fmt tidy run down logs smoke clean

# Local build/test of the whole module.
build:
	go build ./...

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

# Bring the full stack up (NATS + 4 services) and tail logs.
run:
	docker compose up --build

down:
	docker compose down -v

logs:
	docker compose logs -f

# End-to-end smoke test against a running stack (see scripts/smoke.sh).
smoke:
	./scripts/smoke.sh

clean:
	rm -rf ./bin
	go clean
