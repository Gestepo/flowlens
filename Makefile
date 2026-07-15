.PHONY: test test-go test-web build web/dist bin/flowlens-server bin/flowlens-agent install-server install-agent

test: test-go test-web

test-go:
	go test ./...

test-web:
	cd web && npm test

build: bin/flowlens-server bin/flowlens-agent web/dist

web/dist:
	cd web && npm run build

bin/flowlens-server:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/flowlens-server ./cmd/flowlens-server

bin/flowlens-agent:
	mkdir -p bin
	CGO_ENABLED=1 go build -o bin/flowlens-agent ./cmd/flowlens-agent

install-server: build
	./scripts/install-server.sh

install-agent: build
	./scripts/install-agent.sh
