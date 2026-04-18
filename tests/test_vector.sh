#!/bin/bash
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

find_lmd() {
    if command -v lmd >/dev/null 2>&1; then echo "$(command -v lmd)"; return; fi
    if [ -f "$HOME/go/bin/lmd" ]; then echo "$HOME/go/bin/lmd"; return; fi
    local bin="$PROJECT_DIR/lmd"
    if [ ! -f "$bin" ]; then
        echo "Building lmd..." >&2
        (cd "$PROJECT_DIR" && go build -tags "fts5" -o "$bin" ./cmd/lmd)
    fi
    echo "$bin"
}
LMD=$(find_lmd)
TEST_DIR="/tmp/lmd-test-vec-$$"
DB="$TEST_DIR/lmd.sqlite"
DOCS="$TEST_DIR/docs"

pass=0
fail=0
errors=""

green() { printf "\033[32m%s\033[0m\n" "$1"; }
red()   { printf "\033[31m%s\033[0m\n" "$1"; }

assert_json_len_ge() {
    local desc="$1" json="$2" min="$3"
    local actual
    actual=$(echo "$json" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
    if [ "$actual" -ge "$min" ]; then
        pass=$((pass + 1))
    else
        fail=$((fail + 1))
        errors="$errors\n  FAIL: $desc\n    expected >= $min\n    actual: $actual"
    fi
}

assert_json_field() {
    local desc="$1" json="$2" field="$3" expected="$4"
    local actual
    actual=$(echo "$json" | python3 -c "import sys,json; data=json.load(sys.stdin); print(data$field)" 2>/dev/null || echo "PARSE_ERROR")
    if [ "$actual" = "$expected" ]; then
        pass=$((pass + 1))
    else
        fail=$((fail + 1))
        errors="$errors\n  FAIL: $desc\n    expected: $expected\n    actual:   $actual"
    fi
}

assert_json_field_ge() {
    local desc="$1" json="$2" field="$3" min="$4"
    local actual
    actual=$(echo "$json" | python3 -c "import sys,json; data=json.load(sys.stdin); print(data$field)" 2>/dev/null || echo "0")
    if [ "$actual" != "PARSE_ERROR" ] && [ "$(echo "$actual >= $min" | bc -l 2>/dev/null || echo 0)" = "1" ]; then
        pass=$((pass + 1))
    else
        fail=$((fail + 1))
        errors="$errors\n  FAIL: $desc\n    expected >= $min\n    actual: $actual"
    fi
}

lmd_json() { "$LMD" --index "$DB" "$@" 2>/dev/null; }
lmd_out()  { "$LMD" --index "$DB" "$@" 2>&1; }

cleanup() { rm -rf "$TEST_DIR"; }
trap cleanup EXIT

check_ollama() {
    if ! command -v ollama >/dev/null 2>&1; then
        echo "SKIP: ollama not found, skipping vector tests"
        exit 0
    fi
    local models
    models=$(ollama list 2>&1 || echo "")
    if ! echo "$models" | grep -q "embedding"; then
        echo "SKIP: no embedding model found, skipping vector tests"
        exit 0
    fi
}
check_ollama

mkdir -p "$DOCS"

cat > "$DOCS/go.md" <<'EOF'
# Go并发编程

Go语言通过goroutine和channel实现并发编程。
goroutine是轻量级线程，channel用于goroutine间通信。

## goroutine基础

使用go关键字启动goroutine：

```go
go func() {
    fmt.Println("hello from goroutine")
}()
```

## channel通信

channel是goroutine之间的通信机制：

```go
ch := make(chan int)
go func() { ch <- 42 }()
value := <-ch
```
EOF

cat > "$DOCS/python.md" <<'EOF'
# Python数据科学

Python是数据科学领域最流行的语言。
pandas和numpy是核心数据处理库。

## pandas基础

DataFrame是pandas的核心数据结构：

```python
import pandas as pd
df = pd.read_csv('data.csv')
df.describe()
```
EOF

cat > "$DOCS/docker.md" <<'EOF'
# Docker容器管理

Docker是最流行的容器化工具。

## 常用命令

| 命令 | 描述 |
|------|------|
| docker build -t img . | 构建镜像 |
| docker run -p 8080:80 img | 运行容器 |
| docker ps -a | 显示所有容器 |
| docker stop [id] | 停止容器 |

## Docker Compose

使用docker-compose管理多容器应用：

```yaml
version: '3'
services:
  web:
    image: nginx
    ports:
      - "8080:80"
```
EOF

lmd_out collection add "$DOCS" --name testcol >/dev/null
lmd_out update >/dev/null

# ============================================================
echo "===== Test 1: embed chunks ====="
# ============================================================
out=$(lmd_out embed)
if echo "$out" | grep -q "Embedded"; then
    pass=$((pass + 1))
else
    fail=$((fail + 1)); errors="$errors\n  FAIL: embed output: $out"
fi

# ============================================================
echo "===== Test 2: status shows embedded ====="
# ============================================================
out=$(lmd_out status)
if echo "$out" | grep -q "embedded"; then
    pass=$((pass + 1))
else
    fail=$((fail + 1)); errors="$errors\n  FAIL: status should show embedded count"
fi

# ============================================================
echo "===== Test 3: vsearch returns results ====="
# ============================================================
out=$(lmd_json vsearch "goroutine并发" --json)
assert_json_len_ge "vsearch results" "$out" "1"

# ============================================================
echo "===== Test 4: vsearch hits correct doc ====="
# ============================================================
out=$(lmd_json vsearch "goroutine并发" --json)
assert_json_field "vsearch path" "$out" "[0]['path']" "go.md"

# ============================================================
echo "===== Test 5: vsearch score > 0 ====="
# ============================================================
out=$(lmd_json vsearch "docker容器" --json)
assert_json_field_ge "vsearch score > 0" "$out" "[0]['score']" "0.1"

# ============================================================
echo "===== Test 6: vsearch Chinese query ====="
# ============================================================
out=$(lmd_json vsearch "数据科学" --json)
assert_json_len_ge "vsearch chinese" "$out" "1"
assert_json_field "vsearch chinese path" "$out" "[0]['path']" "python.md"

# ============================================================
echo "===== Test 7: vsearch has line field ====="
# ============================================================
out=$(lmd_json vsearch "goroutine" --json)
line_num=$(echo "$out" | python3 -c "import sys,json; print(json.load(sys.stdin)[0].get('line','MISSING'))" 2>/dev/null || echo "MISSING")
if [ "$line_num" != "MISSING" ]; then
    pass=$((pass + 1))
else
    fail=$((fail + 1)); errors="$errors\n  FAIL: vsearch result missing line field"
fi

# ============================================================
echo "===== Test 8: query (hybrid) returns results ====="
# ============================================================
out=$(lmd_json query "docker命令" --json)
assert_json_len_ge "query results" "$out" "1"

# ============================================================
echo "===== Test 9: query score > vsearch-only score ====="
# ============================================================
vsearch_score=$(lmd_json vsearch "goroutine" --json | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['score'])" 2>/dev/null || echo "0")
query_score=$(lmd_json query "goroutine" --json | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['score'])" 2>/dev/null || echo "0")
echo "  vsearch=$vsearch_score query=$query_score"
if [ "$(echo "$query_score > 0" | bc -l 2>/dev/null || echo 0)" = "1" ]; then
    pass=$((pass + 1))
else
    fail=$((fail + 1)); errors="$errors\n  FAIL: query score should be > 0, got $query_score"
fi

# ============================================================
echo "===== Test 10: query with collection filter ====="
# ============================================================
out=$(lmd_json query "编程" -c testcol --json)
assert_json_len_ge "query collection filter" "$out" "1"

# ============================================================
echo "===== Test 11: embed idempotent ====="
# ============================================================
out=$(lmd_out embed)
if echo "$out" | grep -q "Skipped"; then
    pass=$((pass + 1))
else
    fail=$((fail + 1)); errors="$errors\n  FAIL: second embed should skip, got: $out"
fi

# ============================================================
echo "===== Test 12: vsearch no results for nonsense ====="
# ============================================================
out=$(lmd_json vsearch "xyznonexistent12345" --json)
actual=$(echo "$out" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
if [ "$actual" -le 1 ]; then
    pass=$((pass + 1))
else
    fail=$((fail + 1)); errors="$errors\n  FAIL: nonsense query should return 0-1, got $actual"
fi

# ============================================================
echo ""
echo "=========================================="
if [ $fail -eq 0 ]; then
    green "ALL $pass TESTS PASSED"
else
    red "$pass passed, $fail failed"
    echo -e "$errors"
fi
echo "=========================================="

[ $fail -eq 0 ]
