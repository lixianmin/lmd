# rebuildMu Lock Refactor — RWMutex 正确用法

## 问题

`rebuildMu` 原本对所有后台操作使用 `Lock()`（写锁），导致：

1. `syncIndex` 持写锁期间，所有 HTTP handler（即使纯读）被阻塞（Phase 2 在 handler 外包裹了 `RLock()`）
2. 为修此问题，`syncIndexUnlocked` 在 per-collection 索引期间释放并重获写锁，引入并发写入冲突
3. `embedChunks` 不必要地持写锁，延迟了 syncIndex

根本原因：误用了 `Lock()` 而非 `RLock()`。

## 原理

```
RWMutex 语义：
  RLock() — 共享锁，多个 reader 同时持有，不互相阻塞
  Lock()  — 独占锁，阻塞所有 RLock 和 Lock

SQLite WAL 模式语义：
  读操作不阻塞写操作
  写操作不阻塞读操作
  多个写操作由 SQLite 内部序列化（busy_timeout=5000ms）
```

应用层不需要为读操作加锁（SQLite WAL 已保障），也不需要在多写操作间加互斥锁（SQLite WAL 已序列化——`busy_timeout=5000` 确保写操作等待而非立即失败）。

唯一需要应用层互斥的：`handleRebuild` 摧毁并重建数据库——在此期间其他操作不应访问已销毁的 DB。

## 设计

| 操作 | 锁类型 | 理由 |
|------|--------|------|
| `syncIndex` | `RLock` | 与 embedChunks 共享，不被 rebuild 打断 |
| `syncIndexUnlocked` | 无（per-collection 不再释放重获） | RLock 不阻塞读，无需释放；与 embedChunks 不冲突 |
| `embedChunks` | 无 | SQLite WAL 序列化写入，不与 syncIndex 冲突 |
| `handleCollectionAdd` | `RLock` | 防止 rebuild 期间调 UpdateCollection |
| `handleRebuild` | `Lock` | 独占，等所有 RLock 释放后摧毁 DB |
| 所有 HTTP 读请求 | 无 | SQLite WAL 已保障一致性 |

## 改动

### `internal/daemon/daemon.go`

**syncIndex (line 219-223):**
```go
// 改前
func (my *Daemon) syncIndex() {
    my.rebuildMu.Lock()
    defer my.rebuildMu.Unlock()
    my.syncIndexUnlocked()
}

// 改后
func (my *Daemon) syncIndex() {
    my.rebuildMu.RLock()
    defer my.rebuildMu.RUnlock()
    my.syncIndexUnlocked()
}
```

**syncIndexUnlocked (line 235-238):**
```go
// 删除这 4 行（注释 + Unlock + Lock）：
// Release lock during per-collection indexing to avoid blocking HTTP handlers
my.rebuildMu.Unlock()
result, err := my.indexer.UpdateCollection(...)
my.rebuildMu.Lock()

// 改为直调：
result, err := my.indexer.UpdateCollection(...)
```

**embedChunks (line 253-254):**
```go
// 删除这 2 行：
my.rebuildMu.Lock()
defer my.rebuildMu.Unlock()
```

### `internal/daemon/daemon_routes.go`
无改动。`handleCollectionAdd` (RLock/RUnlock) 和 `handleRebuild` (Lock/Unlock) 已正确。

## 并发安全性

### 场景 1：syncIndex 与 embedChunks 并发
```
syncIndex → RLock → UpdateCollection (WAL write) → RUnlock
embedChunks → GetUnembeddedCount → EmbedBatch (WAL write)
```
SQLite WAL 的 `busy_timeout=5000` 自动序列化写操作。不冲突，不阻塞。

### 场景 2：rebuild 期间 syncIndex 触发
```
rebuild: Lock() → 摧毁DB → Init() → AddCollections → Unlock → syncIndex
syncIndex: RLock() ← 阻塞等 rebuild 释放 Lock
```
syncIndex 等待 rebuild 完成后再运行。

### 场景 3：rebuild 期间 embedChunks 触发
```
embedChunks: GetUnembeddedCount() → DB 已被 rebuild 摧毁
```
DB 已关闭，QueryRow 返回 error。embedChunks log error 后返回，不会 panic。rebuild 完成后 embedChunks 正常。

### 场景 4：两个 syncIndex 并发
不可能——goLoop 是单 goroutine 60s ticker。

### 场景 5：handleCollectionAdd 与 syncIndex 并发同一 collection
两个 `UpdateCollection` 同时运行。当前 `InsertChunks` 使用 `INSERT` 非 `REPLACE`，第二个写入遇 `UNIQUE(doc_id, seq)` 冲突会错误回滚整个文档的 chunk 事务。

**缓解**：概率极低（用户手动 add 恰好与 60s ticker 重合）。后续可将 `InsertChunks` 改为 `INSERT OR IGNORE`，不在本次 scope。
