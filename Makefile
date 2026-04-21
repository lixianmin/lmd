.PHONY: build install both test test-verbose vet clean tidy fmt lint e2e integration integration-basic integration-vector submodule

BINARY  = lmd
PKG     = github.com/lixianmin/lmd
CMD     = $(PKG)/cmd/lmd
TAGS    = fts5
GO      = go
LDFLAGS = -s -w
MOD     = -mod=mod
LLAMA_DIR = llama-go
ENV     = LIBRARY_PATH=$$PWD/$(LLAMA_DIR) C_INCLUDE_PATH=$$PWD/$(LLAMA_DIR) CGO_LDFLAGS="-lggml-metal -lggml-blas"

submodule:
	@if [ ! -f $(LLAMA_DIR)/libllama.a ]; then \
		echo "Initializing llama-go submodule..."; \
		git submodule update --init --recursive; \
		cd $(LLAMA_DIR) && mkdir -p build && cd build && \
			cmake ../llama.cpp -DGGML_METAL=ON -DCMAKE_BUILD_TYPE=Release -DLLAMA_CURL=OFF -DBUILD_SHARED_LIBS=OFF && \
			cmake --build . --config Release -j && cd ../..; \
		cp $(LLAMA_DIR)/build/src/libllama.a $(LLAMA_DIR)/ 2>/dev/null || true; \
		cp $(LLAMA_DIR)/build/ggml/src/libggml.a $(LLAMA_DIR)/ 2>/dev/null || true; \
		cp $(LLAMA_DIR)/build/ggml/src/libggml-base.a $(LLAMA_DIR)/ 2>/dev/null || true; \
		cp $(LLAMA_DIR)/build/ggml/src/libggml-cpu.a $(LLAMA_DIR)/ 2>/dev/null || true; \
		cp $(LLAMA_DIR)/build/ggml/src/ggml-metal/libggml-metal.a $(LLAMA_DIR)/ 2>/dev/null || true; \
		cp $(LLAMA_DIR)/build/ggml/src/ggml-blas/libggml-blas.a $(LLAMA_DIR)/ 2>/dev/null || true; \
		cp $(LLAMA_DIR)/build/common/libcommon.a $(LLAMA_DIR)/ 2>/dev/null || true; \
		echo "llama-go build complete."; \
	fi

build: submodule
	-./$(BINARY) stop 2>/dev/null || true
	$(ENV) $(GO) build -tags "$(TAGS)" -ldflags "$(LDFLAGS)" $(MOD) -o $(BINARY) $(CMD)

install: submodule
	-$(GO) env GOPATH/bin/lmd stop 2>/dev/null || true
	$(ENV) $(GO) install -tags "$(TAGS)" -ldflags "$(LDFLAGS)" $(MOD) $(CMD)

both: build install

test: submodule
	$(ENV) $(GO) test -tags "$(TAGS)" -count=1 $(MOD) ./...

test-verbose: submodule
	$(ENV) $(GO) test -tags "$(TAGS)" -count=1 -v $(MOD) ./...

vet: submodule
	$(ENV) $(GO) vet -tags "$(TAGS)" $(MOD) ./...

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
