.PHONY: build install test vet clean tidy fmt lint e2e integration integration-basic integration-vector

BINARY  = lmd
PKG     = github.com/lixianmin/lmd
CMD     = $(PKG)/cmd/lmd
TAGS    = fts5
GO      = go
LDFLAGS = -s -w

build:
	$(GO) build -tags "$(TAGS)" -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

install:
	$(GO) install -tags "$(TAGS)" -ldflags "$(LDFLAGS)" $(CMD)

test:
	$(GO) test -tags "$(TAGS)" -count=1 ./...

test-verbose:
	$(GO) test -tags "$(TAGS)" -count=1 -v ./...

vet:
	$(GO) vet -tags "$(TAGS)" ./...

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
	@./$(BINARY) --index /tmp/lmd-e2e/test.sqlite collection add /tmp/lmd-e2e/docs --name docs
	@./$(BINARY) --index /tmp/lmd-e2e/test.sqlite update
	@./$(BINARY) --index /tmp/lmd-e2e/test.sqlite embed
	@./$(BINARY) --index /tmp/lmd-e2e/test.sqlite search "并发"
	@./$(BINARY) --index /tmp/lmd-e2e/test.sqlite vsearch "并发编程"
	@./$(BINARY) --index /tmp/lmd-e2e/test.sqlite status
	@rm -rf /tmp/lmd-e2e

integration-basic: install
	bash tests/test_basic.sh

integration-vector: install
	bash tests/test_vector.sh

integration: integration-basic

all: lint test integration-basic
