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
TEST_DIR="/tmp/lmd-test-$$"
DB="$TEST_DIR/lmd.sqlite"
DOCS="$TEST_DIR/docs"

pass=0
fail=0
errors=""

green() { printf "\033[32m%s\033[0m\n" "$1"; }
red()   { printf "\033[31m%s\033[0m\n" "$1"; }

assert() {
    local desc="$1" actual="$2" expected="$3"
    if [ "$actual" = "$expected" ]; then
        pass=$((pass + 1))
    else
        fail=$((fail + 1))
        errors="$errors\n  FAIL: $desc\n    expected: $expected\n    actual:   $actual"
    fi
}

assert_contains() {
    local desc="$1" haystack="$2" needle="$3"
    if echo "$haystack" | grep -q "$needle"; then
        pass=$((pass + 1))
    else
        fail=$((fail + 1))
        errors="$errors\n  FAIL: $desc\n    expected to contain: $needle\n    actual: ${haystack:0:200}"
    fi
}

assert_not_contains() {
    local desc="$1" haystack="$2" needle="$3"
    if echo "$haystack" | grep -q "$needle"; then
        fail=$((fail + 1))
        errors="$errors\n  FAIL: $desc\n    expected NOT to contain: $needle"
    else
        pass=$((pass + 1))
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

assert_json_len() {
    local desc="$1" json="$2" expected="$3"
    local actual
    actual=$(echo "$json" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "PARSE_ERROR")
    if [ "$actual" = "$expected" ]; then
        pass=$((pass + 1))
    else
        fail=$((fail + 1))
        errors="$errors\n  FAIL: $desc\n    expected count: $expected\n    actual: $actual"
    fi
}

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

lmd_json() { "$LMD" --index "$DB" "$@" 2>/dev/null; }
lmd_out()  { "$LMD" --index "$DB" "$@" 2>&1; }

cleanup() { rm -rf "$TEST_DIR"; }
trap cleanup EXIT

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

## select多路复用

select可以同时监听多个channel。
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

## numpy数组操作

```python
import numpy as np
arr = np.array([1, 2, 3])
arr.mean()
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
| docker rm [id] | 删除容器 |
| docker logs [id] | 查看日志 |
| docker exec -it [id] bash | 进入容器 |
| docker system prune -a | 清理磁盘 |

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

## Docker Swarm

Docker Swarm是内置的容器编排工具。
EOF

# ============================================================
echo "===== Test 1: collection add ====="
out=$(lmd_out collection add "$DOCS" --name testcol)
assert_contains "collection add" "$out" "added"

out=$(lmd_out collection list)
assert_contains "collection list" "$out" "testcol"

# ============================================================
echo "===== Test 2: collection add duplicate ====="
if lmd_out collection add "$DOCS" --name testcol >/dev/null 2>&1; then
    fail=$((fail + 1)); errors="$errors\n  FAIL: duplicate add should fail"
else
    pass=$((pass + 1))
fi

# ============================================================
echo "===== Test 3: update (index) ====="
out=$(lmd_out update)
assert_contains "update indexed" "$out" "indexed=3"

out=$(lmd_out status)
assert_contains "status docs" "$out" "3 documents"

# ============================================================
echo "===== Test 4: update idempotent ====="
out=$(lmd_out update)
assert_contains "update unchanged" "$out" "unchanged=3"

# ============================================================
echo "===== Test 5: collection rename ====="
out=$(lmd_out collection rename testcol testcol2)
assert_contains "rename" "$out" "renamed"

out=$(lmd_out collection list)
assert_contains "list renamed" "$out" "testcol2"

lmd_out collection rename testcol2 testcol >/dev/null

# ============================================================
echo "===== Test 6: search BM25 ====="
out=$(lmd_json search "goroutine" --json)
assert_json_len "search goroutine" "$out" "1"
assert_json_field "search goroutine path" "$out" "[0]['path']" "go.md"

# ============================================================
echo "===== Test 7: search Chinese ====="
out=$(lmd_json search "并发" --json)
assert_json_len_ge "search chinese" "$out" "1"

# ============================================================
echo "===== Test 8: search with collection filter ====="
out=$(lmd_json search "docker" -c testcol --json)
assert_json_len_ge "search collection filter" "$out" "1"

# ============================================================
echo "===== Test 9: search no results ====="
out=$(lmd_json search "xyznonexistent12345" --json)
assert_json_len "search no results" "$out" "0"

# ============================================================
echo "===== Test 10: get by path ====="
out=$(lmd_out get testcol/go.md)
assert_contains "get path title" "$out" "Go"
assert_contains "get path body" "$out" "goroutine"

# ============================================================
echo "===== Test 11: get by docid ====="
docid=$(lmd_json search "goroutine" --json | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['doc_id'])" 2>/dev/null)
out=$(lmd_out get "#$docid" --full)
assert_contains "get docid" "$out" "goroutine"

# ============================================================
echo "===== Test 12: get --from --lines ====="
out=$(lmd_out get testcol/go.md --from 2 --lines 2)
line_count=$(echo "$out" | wc -l | tr -d ' ')
if [ "$line_count" -le 8 ]; then
    pass=$((pass + 1))
else
    fail=$((fail + 1)); errors="$errors\n  FAIL: get --from --lines: expected <=8 lines, got $line_count"
fi

# ============================================================
echo "===== Test 13: update detects file change ====="
cat > "$DOCS/go.md" <<'EOF'
# Go并发编程进阶

Go语言通过goroutine和channel实现并发编程。
新增的sync包提供了互斥锁等同步原语。

## sync.Mutex

```go
var mu sync.Mutex
mu.Lock()
// critical section
mu.Unlock()
```
EOF

out=$(lmd_out update)
assert_contains "update change" "$out" "updated=1"

out=$(lmd_json search "Mutex" --json)
assert_json_len_ge "search Mutex" "$out" "1"

# ============================================================
echo "===== Test 14: update detects file deletion ====="
rm "$DOCS/docker.md"
out=$(lmd_out update)
assert_contains "update deletion" "$out" "removed=1"

out=$(lmd_json search "docker build" --json)
assert_json_len "no docker results" "$out" "0"

cat > "$DOCS/docker.md" <<'EOF'
# Docker容器管理
Docker是最流行的容器化工具。
docker build, docker run, docker ps, docker stop.
EOF
lmd_out update >/dev/null

# ============================================================
echo "===== Test 15: search result has line field ====="
out=$(lmd_json search "goroutine" --json)
line_num=$(echo "$out" | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['line'])" 2>/dev/null || echo "NONE")
if [ "$line_num" != "NONE" ]; then
    pass=$((pass + 1))
else
    fail=$((fail + 1)); errors="$errors\n  FAIL: search result missing line field"
fi

# ============================================================
echo "===== Test 16: search limit ====="
out=$(lmd_json search "goroutine" --limit 1 --json)
assert_json_len "search limit" "$out" "1"

# ============================================================
echo "===== Test 17: collection remove ====="
mkdir -p "$TEST_DIR/docs2"
echo "# Test Doc" > "$TEST_DIR/docs2/test.md"
lmd_out collection add "$TEST_DIR/docs2" --name toremove >/dev/null
lmd_out update >/dev/null

out=$(lmd_out collection remove toremove)
assert_contains "collection remove" "$out" "removed"

out=$(lmd_out collection list)
assert_not_contains "removed gone" "$out" "toremove"

# ============================================================
echo "===== Test 18: output formats ====="
out=$(lmd_out search "python" --format text)
assert_contains "text format" "$out" "python.md"

out=$(lmd_out search "python" --format csv)
assert_contains "csv format" "$out" "python.md"

out=$(lmd_out search "python" --format md)
assert_contains "md format" "$out" "python.md"

# ============================================================
echo "===== Test 19: multi-collection search ====="
mkdir -p "$TEST_DIR/docs3"
cat > "$TEST_DIR/docs3/notes.md" <<'EOF'
# 学习笔记

今天学习了Go语言的goroutine并发编程模型。
goroutine和channel是Go并发的核心概念。
EOF
lmd_out collection add "$TEST_DIR/docs3" --name notes >/dev/null
lmd_out update >/dev/null

out=$(lmd_json search "goroutine" --json)
assert_json_len_ge "cross-collection search" "$out" "2"

# ============================================================
echo "===== Test 20: search collection filter excludes ====="
out=$(lmd_json search "goroutine" -c notes --json)
paths=$(echo "$out" | python3 -c "import sys,json; print(' '.join(h['path'] for h in json.load(sys.stdin)))" 2>/dev/null || echo "")
assert_contains "filter includes notes" "$paths" "notes.md"

# ============================================================
echo "===== Test 21: rebuild preserves collections ====="
out=$(lmd_out rebuild)
assert_contains "rebuild reset" "$out" "reset"

out=$(lmd_out collection list)
assert_contains "rebuild testcol" "$out" "testcol"
assert_contains "rebuild notes" "$out" "notes"

# ============================================================
echo "===== Test 22: rebuild reindexes ====="
out=$(lmd_out status)
assert_contains "rebuild status docs" "$out" "documents"

out=$(lmd_json search "goroutine" --json)
assert_json_len_ge "rebuild reindex" "$out" "1"

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
