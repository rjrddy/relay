GO ?= go
PY ?= .venv/bin/python
export GOPATH := $(CURDIR)/.cache/gopath
export GOCACHE := $(CURDIR)/.cache/gobuild
.PHONY: setup build lint test bench bench-long demo scan
setup:
	python3 -m venv .venv
	$(PY) -m pip install --upgrade pip
	$(PY) -m pip install -e '.[dev]'
	$(GO) mod download
	npm ci --prefix apps/web
build:
	$(GO) build -trimpath -o bin/relay ./cmd/relay
	npm run build --prefix apps/web
lint:
	$(PY) scripts/check_go_format.py "$(GO)"
	$(GO) vet ./...
	npm run lint --prefix apps/web
	npm exec --prefix apps/web -- prettier --check apps/web/src
	$(PY) -m ruff check sdk/python scripts
	$(PY) -m ruff format --check sdk/python scripts
test:
	@mkdir -p docs/results
	$(GO) test -race -count=1 -json ./... > docs/results/tests.jsonl || ($(PY) scripts/test_summary.py docs/results/tests.jsonl; exit 1)
	$(PY) scripts/test_summary.py docs/results/tests.jsonl
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
