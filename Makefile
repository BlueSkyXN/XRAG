.PHONY: build test race vet clean web web-dev
SHA := $(shell git rev-parse HEAD 2>/dev/null || echo dev)
LDFLAGS := -X github.com/BlueSkyXN/XRAG/core.Commit=$(SHA)
build:
	mkdir -p bin
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/xrag-server ./cmd/xrag-server
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/xrag ./cmd/xrag
test:
	go test -count=1 ./...
race:
	go test -race -count=1 ./...
vet:
	go vet ./...
clean:
	rm -rf bin
web:
	cd webapp && npm ci && npm run build
web-dev:
	cd webapp && npm run dev
