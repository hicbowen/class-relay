.PHONY: install frontend build run check test release

install:
	cd frontend && npm ci

frontend:
	cd frontend && npm run build
	rsync -a --delete --exclude='.gitkeep' frontend/dist/ web/dist/

build: frontend
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/classrelay ./cmd/classrelay

run: build
	./bin/classrelay

check:
	cd frontend && npm run lint && npm run check
	go vet ./cmd/... ./internal/... ./web/...

test:
	go test ./cmd/... ./internal/... ./web/...

release: frontend
	sh scripts/release.sh
