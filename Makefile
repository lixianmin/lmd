.PHONY: build install both test test-verbose vet clean tidy fmt lint e2e integration integration-basic integration-vector rebuild

BINARY  = lmd
PKG     = github.com/lixianmin/lmd
CMD     = $(PKG)/cmd/lmd
TAGS    = fts5
GO      = go
LDFLAGS = -s -w
MOD     = -mod=mod

build:
	-./$(BINARY) stop 2>/dev/null || true
	$(GO) build -tags "$(TAGS)" -ldflags "$(LDFLAGS)" $(MOD) -o $(BINARY) $(CMD)

install:
	-$(GO) env GOPATH/bin/lmd stop 2>/dev/null || true
	$(GO) install -tags "$(TAGS)" -ldflags "$(LDFLAGS)" $(MOD) $(CMD)

both: build install

test:
	$(GO) test -tags "$(TAGS)" -count=1 $(MOD) ./...

test-verbose:
	$(GO) test -tags "$(TAGS)" -count=1 -v $(MOD) ./...

vet:
	$(GO) vet -tags "$(TAGS)" $(MOD) ./...

tidy:
	$(GO) mod tidy

fmt:
	gofmt -w .

lint: vet fmt

clean:
	rm -f $(BINARY)

e2e: build
	@rm -rf /tmp/lmd-e2e
	@mkdir -p /tmp/lmd-e2e/docs
	@echo '# Go并发编程\n\nGo语言通过goroutine和channel实现并发编程。\ngoroutine是轻量级线程，channel用于goroutine间通信。' > /tmp/lmd-e2e/docs/go.md
	@echo '# Python数据科学\n\nPython是数据科学领域最流行的语言。\npandas和numpy是核心数据处理库。' > /tmp/lmd-e2e/docs/python.md
	@./$(BINARY) collection add /tmp/lmd-e2e/docs --name docs
	@./$(BINARY) search "并发"
	@./$(BINARY) status
	@rm -rf /tmp/lmd-e2e

integration-basic: install
	bash tests/test_basic.sh

integration-vector: install
	bash tests/test_vector.sh

integration: integration-basic

all: lint test integration-basic


rebuild:
	@echo "=== Capturing collections ==="
	@-./$(BINARY) status --json > /tmp/lmd-rebuild.json 2>/dev/null
	@echo "=== Building ==="
	@-./$(BINARY) stop 2>/dev/null || true
	@$(GO) build -tags "$(TAGS)" -ldflags "$(LDFLAGS)" $(MOD) -o $(BINARY) $(CMD)
	@sleep 1
	@echo "=== Deleting database ==="
	@rm -f $$HOME/.cache/lmd/index.sqlite
	@echo "=== Installing and starting ==="
	@$(GO) install -tags "$(TAGS)" -ldflags "$(LDFLAGS)" $(MOD) $(CMD)
	@$(shell $(GO) env GOPATH)/bin/lmd daemon-start &
	@sleep 3
	@echo "=== Re-adding collections ==="
	@python3 scripts/re_add_collections.py /tmp/lmd-rebuild.json $(shell $(GO) env GOPATH)/bin/lmd || \
		echo "Note: collections re-add may fail if no previous collections existed"
	@rm -f /tmp/lmd-rebuild.json
	@echo "=== Rebuild complete ==="
