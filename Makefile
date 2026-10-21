GO ?= go
PY ?= python3
export GOPATH := $(CURDIR)/.cache/gopath
export GOCACHE := $(CURDIR)/.cache/gobuild
.PHONY: setup build lint test bench bench-long demo scan
setup:
	$(GO) mod download
	npm ci --prefix apps/web
build:
	$(GO) build -trimpath -o bin/relay ./cmd/relay
	npm run build --prefix apps/web
lint:
	@test -z "$$($(GO) fmt ./...)" || (echo 'Go files were not formatted'; exit 1)
	$(GO) vet ./...
	npm run lint --prefix apps/web
	npm exec --prefix apps/web -- prettier --check apps/web/src
	$(PY) -m compileall -q sdk/python scripts
test:
	$(GO) test -race -count=1 -json ./... > /tmp/relay-tests.json
	$(PY) scripts/test_summary.py /tmp/relay-tests.json
bench: build
	$(GO) run ./cmd/bench --jobs 100 --out load-100
	$(GO) run ./cmd/bench --jobs 500 --out load-500
	$(GO) run ./cmd/bench --jobs 1000 --out load-1000
	$(GO) run ./cmd/bench --jobs 300 --chaos --duration 60s --out chaos
bench-long: build
	$(GO) run ./cmd/bench --jobs 300 --chaos --duration 1h --out chaos-hour
demo:
	docker compose up --build
scan:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...
	npm audit --prefix apps/web --audit-level=high
