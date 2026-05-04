# Memory 统一存储实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将记忆系统合并到 documents+chunks 表，使用 `@episodic`/`@knowledge` collection 区分，移除衰减逻辑，统一搜索入口。

**Architecture:** 移除 memories/memories_fts/memories_vec 三张表；记忆存储为 document+chunk；memory/add/delete/update 操作复用 chunks 的 FTS+Vector 索引；query 统一使用现有 /query 接口。

**Tech Stack:** Go, SQLite + sqlite-vec, FTS5

---

### Task 1: 更新 dao/db_init.go — Schema 迁移与旧表移除

**Files:**
- Modify: `internal/dao/db_init.go`

- [ ] **Step 1: 移除 memories 表定义，添加迁移逻辑**

修改 `createTables()`：从 stmts 中删除 memories/memories_fts/memories_vec 三条 CREATE。在函数末尾 `createTables` 返回前调用迁移函数。

```go
func createTables() error {
	stmts := []string{
		// collections, documents, chunks, chunks_fts, chunks_vec, idx 不变
		// 删除 memories, memories_fts, memories_vec 三条
	}
	for _, s := range stmts {
		if _, err := DB.db.Exec(s); err != nil {
			return err
		}
	}

	_, _ = DB.db.Exec("ALTER TABLE documents ADD COLUMN file_mod_time INTEGER DEFAULT 0")

	// 迁移旧 memories 数据到 documents+chunks，然后移除旧表
	if err := migrateMemories(); err != nil {
		return err
	}
	return nil
}

func migrateMemories() error {
	// 检查旧表是否存在
	var count int
	err := DB.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='memories'").Scan(&count)
	if err != nil || count == 0 {
		return nil // 表不存在，无需迁移
	}

	// 读取所有旧记忆
	rows, err := DB.db.Query("SELECT id, content, type, created_at FROM memories")
	if err != nil {
		return err
	}
	defer rows.Close()

	type oldMem struct {
		id        int64
		content   string
		memType   string
		createdAt string
	}
	var mems []oldMem
	for rows.Next() {
		var m oldMem
		if err := rows.Scan(&m.id, &m.content, &m.memType, &m.createdAt); err != nil {
			return err
		}
		mems = append(mems, m)
	}
	rows.Close()

	if len(mems) == 0 {
		// 空表直接删
		DB.db.Exec("DROP TABLE IF EXISTS memories_fts")
		DB.db.Exec("DROP TABLE IF EXISTS memories_vec")
		DB.db.Exec("DROP TABLE IF EXISTS memories")
		return nil
	}

	// 逐条迁移
	for _, m := range mems {
		collection := "@episodic"
		if m.memType == "fact" || m.memType == "relation" {
			collection = "@knowledge"
		}

		hash := hex.EncodeToString(sha256Hash([]byte(m.content)))
		docid := "mem-" + hash[:12]
		path := "/@memory/" + hash[:12]
		title := truncateContent(m.content, 80)

		res, err := DB.db.Exec(
			"INSERT INTO documents (docid, collection, path, title, body, hash) VALUES (?, ?, ?, ?, ?, ?)",
			docid, collection, path, title, m.content, hash,
		)
		if err != nil {
			logo.Warn("migrateMemories: insert doc failed for id=%d: %s", m.id, err)
			continue
		}
		docId, _ := res.LastInsertId()

		_, err = DB.db.Exec(
			"INSERT INTO chunks (doc_id, seq, content, position, hash) VALUES (?, 1, ?, 1, ?)",
			docId, m.content, hash,
		)
		if err != nil {
			logo.Warn("migrateMemories: insert chunk failed for id=%d: %s", m.id, err)
		}
	}

	// 删除旧表
	DB.db.Exec("DROP TABLE IF EXISTS memories_fts")
	DB.db.Exec("DROP TABLE IF EXISTS memories_vec")
	DB.db.Exec("DROP TABLE IF EXISTS memories")

	logo.Info("migrateMemories: migrated %d memories", len(mems))
	return nil
}
```

需要导入 `crypto/sha256` 和 `encoding/hex`。

**现有 tests 会失败**（memories 表被删除）。本任务仅做 schema 变更，后续任务会重写 DAO/Service 使测试通过。

- [ ] **Step 2: 验证 schema 迁移**

```bash
go test ./internal/dao/ -run TestMigrateMemories -v -count=1
```

预期：DB 初始化不报错，旧 memories 表不存在时直接跳过。

- [ ] **Step 3: Commit**

```bash
git add internal/dao/db_init.go
git commit -m "feat: remove memories tables, add migration to documents+chunks"
```

---

### Task 2: 重写 dao/memory.go — 基于 documents+chunks 操作

**Files:**
- Modify: `internal/dao/memory.go`

- [ ] **Step 1: 重写 MemoryRecord 和相关函数**

删除全部旧代码，替换为：

```go
package dao

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

type MemoryRecord struct {
	Id        int64
	DocId     int64
	Content   string
	Collection string
	CreatedAt time.Time
}

func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

const memoryCollectionEpisodic = "@episodic"

func InsertMemory(content string) (int64, error) {
	hash := hex.EncodeToString(sha256Hash([]byte(content)))
	docid := "mem-" + hash[:12]
	path := "/@memory/" + hash[:12]
	title := truncateContent(content, 80)

	var docId int64
	err := withTransaction(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			"INSERT INTO documents (docid, collection, path, title, body, hash) VALUES (?, ?, ?, ?, ?, ?)",
			docid, memoryCollectionEpisodic, path, title, content, hash,
		)
		if err != nil {
			return err
		}
		docId, _ = res.LastInsertId()

		_, err = tx.Exec(
			"INSERT INTO chunks (doc_id, seq, content, position, hash) VALUES (?, 1, ?, 1, ?)",
			docId, content, hash,
		)
		if err != nil {
			return err
		}

		// 插入 FTS（chunks 表有 trigger 或手动写入 FTS）
		var chunkId int64
		tx.QueryRow("SELECT last_insert_rowid()").Scan(&chunkId)
		_, err = tx.Exec("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)", chunkId, content)
		return err
	})
	return docId, err
}

// InsertMemoryFull 插入记忆并写入 FTS（完整事务）
func InsertMemoryFull(content string) (int64, int64, error) {
	hash := hex.EncodeToString(sha256Hash([]byte(content)))
	docid := "mem-" + hash[:12]
	path := "/@memory/" + hash[:12]
	title := truncateContent(content, 80)

	var docId, chunkId int64
	err := withTransaction(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			"INSERT INTO documents (docid, collection, path, title, body, hash) VALUES (?, ?, ?, ?, ?, ?)",
			docid, memoryCollectionEpisodic, path, title, content, hash,
		)
		if err != nil {
			return err
		}
		docId, _ = res.LastInsertId()

		res, err = tx.Exec(
			"INSERT INTO chunks (doc_id, seq, content, position, hash) VALUES (?, 1, ?, 1, ?)",
			docId, content, hash,
		)
		if err != nil {
			return err
		}
		chunkId, _ = res.LastInsertId()

		_, err = tx.Exec("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)", chunkId, content)
		return err
	})
	if err != nil {
		return 0, 0, err
	}
	return docId, chunkId, nil
}

func GetMemoryByID(docId int64) (*MemoryRecord, error) {
	row := withQueryRow(
		"SELECT d.id, d.body, d.collection, d.created_at FROM documents d WHERE d.id=?",
		docId,
	)
	var rec MemoryRecord
	if err := row.Scan(&rec.Id, &rec.Content, &rec.Collection, &rec.CreatedAt); err != nil {
		return nil, err
	}
	rec.DocId = rec.Id
	return &rec, nil
}

func DeleteMemory(docId int64) error {
	// 级联删除分两步：先清理 vec+fts，再删 document
	return withTransaction(func(tx *sql.Tx) error {
		// 获取所有关联的 chunk id
		chunkRows, err := tx.Query("SELECT id FROM chunks WHERE doc_id=?", docId)
		if err != nil {
			return err
		}
		var chunkIds []int64
		for chunkRows.Next() {
			var cid int64
			if err := chunkRows.Scan(&cid); err != nil {
				chunkRows.Close()
				return err
			}
			chunkIds = append(chunkIds, cid)
		}
		chunkRows.Close()

		for _, cid := range chunkIds {
			tx.Exec("DELETE FROM chunks_vec WHERE chunk_id=?", cid)
			tx.Exec("DELETE FROM chunks_fts WHERE rowid=?", cid)
		}
		tx.Exec("DELETE FROM chunks WHERE doc_id=?", docId)

		res, err := tx.Exec("DELETE FROM documents WHERE id=?", docId)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

func UpdateMemory(docId int64, content string) error {
	hash := hex.EncodeToString(sha256Hash([]byte(content)))
	docid := "mem-" + hash[:12]
	title := truncateContent(content, 80)

	return withTransaction(func(tx *sql.Tx) error {
		// 删除旧 chunks
		chunkRows, err := tx.Query("SELECT id FROM chunks WHERE doc_id=?", docId)
		if err != nil {
			return err
		}
		var chunkIds []int64
		for chunkRows.Next() {
			var cid int64
			if err := chunkRows.Scan(&cid); err != nil {
				chunkRows.Close()
				return err
			}
			chunkIds = append(chunkIds, cid)
		}
		chunkRows.Close()

		for _, cid := range chunkIds {
			tx.Exec("DELETE FROM chunks_vec WHERE chunk_id=?", cid)
			tx.Exec("DELETE FROM chunks_fts WHERE rowid=?", cid)
		}
		tx.Exec("DELETE FROM chunks WHERE doc_id=?", docId)

		// 插入新 chunk
		res, err := tx.Exec(
			"INSERT INTO chunks (doc_id, seq, content, position, hash) VALUES (?, 1, ?, 1, ?)",
			docId, content, hash,
		)
		if err != nil {
			return err
		}
		chunkId, _ := res.LastInsertId()

		_, err = tx.Exec("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)", chunkId, content)
		if err != nil {
			return err
		}

		// 更新 document
		_, err = tx.Exec(
			"UPDATE documents SET docid=?, body=?, hash=?, title=?, updated_at=DATETIME('now', '+8 hours') WHERE id=?",
			docid, content, hash, title, docId,
		)
		return err
	})
}

func ListMemories(limit int) ([]MemoryRecord, error) {
	query := "SELECT d.id, d.body, d.collection, d.created_at FROM documents d WHERE d.collection IN (?, ?) ORDER BY d.created_at DESC"
	args := []any{memoryCollectionEpisodic, "@knowledge"}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := withQuery(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRecord
	for rows.Next() {
		var rec MemoryRecord
		if err := rows.Scan(&rec.Id, &rec.Content, &rec.Collection, &rec.CreatedAt); err != nil {
			return nil, err
		}
		rec.DocId = rec.Id
		results = append(results, rec)
	}
	return results, rows.Err()
}

func truncateContent(content string, maxLen int) string {
	if len([]rune(content)) <= maxLen {
		return content
	}
	runes := []rune(content)
	return string(runes[:maxLen-3]) + "..."
}

func CountMemories() (int, error) {
	var count int
	err := withQueryRow(
		"SELECT COUNT(*) FROM documents WHERE collection IN (?, ?)",
		memoryCollectionEpisodic, "@knowledge",
	).Scan(&count)
	return count, err
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/dao/
```

预期：编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/dao/memory.go
git commit -m "feat: rewrite memory DAO to use documents+chunks tables"
```

---

### Task 3: 重写 service/memory.go — 去除衰减，简化为存储服务

**Files:**
- Modify: `internal/service/memory.go`

- [ ] **Step 1: 替换全部代码**

```go
package service

import (
	"fmt"

	"github.com/lixianmin/lmd/internal/dao"
)

type MemorySearchResult struct {
	ID        int64   `json:"id"`
	Content   string  `json:"content"`
	Collection string `json:"collection"`
	CreatedAt string  `json:"created_at"`
}

type MemoryService struct{}

func NewMemoryService() *MemoryService {
	return &MemoryService{}
}

func (my *MemoryService) Add(content string) (int64, error) {
	if content == "" {
		return 0, fmt.Errorf("content is required")
	}
	return dao.InsertMemory(content)
}

func (my *MemoryService) Delete(id int64) error {
	return dao.DeleteMemory(id)
}

func (my *MemoryService) Update(id int64, content string) error {
	if content == "" {
		return fmt.Errorf("content is required")
	}
	return dao.UpdateMemory(id, content)
}

func (my *MemoryService) List(limit int) ([]MemorySearchResult, error) {
	recs, err := dao.ListMemories(limit)
	if err != nil {
		return nil, err
	}
	results := make([]MemorySearchResult, len(recs))
	for i, r := range recs {
		results[i] = MemorySearchResult{
			ID:        r.Id,
			Content:   r.Content,
			Collection: r.Collection,
			CreatedAt: r.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}
	return results, nil
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/service/
```

- [ ] **Step 3: Commit**

```bash
git add internal/service/memory.go
git commit -m "feat: simplify MemoryService, remove decay, remove Query"
```

---

### Task 4: 更新 daemon — 移除 embedMemories 及相关调用

**Files:**
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: 修改 daemon.go**

删除 `memoryEmbedBatchSize` 常量、`memSvc` 字段和 `embedMemories()` 方法。简化 `NewMemoryService` 调用。

修改点：
1. 删除 `const memoryEmbedBatchSize = 8`
2. 删除 `Daemon` 结构体中 `memSvc *service.MemoryService` 字段
3. `NewDaemon` 中删除 `my.memSvc = service.NewMemoryService(tok, my.provider)`，改为 `_ = service.NewMemoryService()`
4. `goLoop` 中删除 `my.embedMemories()` 调用
5. 删除整个 `embedMemories()` 方法（L261-307）

```go
// 在 Daemon.Start() 中：
my.memSvc = service.NewMemoryService()
```

在 `goLoop` 中：
```go
case <-embedTicker.C:
    my.embedChunks()
    // 删除 my.embedMemories() 调用
    my.provider.ReleaseIfIdle(modelIdleTimeout)
```

- [ ] **Step 2: 编译验证**

```bash
go build ./internal/daemon/
```

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat: remove embedMemories, memories now use chunk embed pipeline"
```

---

### Task 5: 更新 routes.go — 重写 memory handlers，添加 `@` 前缀保护

**Files:**
- Modify: `internal/daemon/routes.go`

- [ ] **Step 1: 重写 memory handlers**

删除 `handleMemoryQuery` 和 `handleMemoryAdd`/`handleMemoryDelete` 旧的实现。添加 `handleMemoryUpdate`。

```go
func (my *Daemon) handleMemoryAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}

	id, err := my.memSvc.Add(req.Content)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	rec, _ := dao.GetMemoryByID(id)
	createdAt := ""
	if rec != nil {
		createdAt = rec.CreatedAt.Format("2006-01-02 15:04:05")
	}

	logo.Info("handleMemoryAdd: id=%d", id)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         id,
		"created_at": createdAt,
	})
}

func (my *Daemon) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.ID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}

	if err := dao.DeleteMemory(req.ID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "memory not found"})
		return
	}

	logo.Info("handleMemoryDelete: id=%d", req.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (my *Daemon) handleMemoryUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      int64  `json:"id"`
		Content string `json:"content"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.ID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}

	if err := my.memSvc.Update(req.ID, req.Content); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	logo.Info("handleMemoryUpdate: id=%d", req.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
```

- [ ] **Step 2: 在 collection handlers 添加 `@` 前缀保护**

在 `handleCollectionAdd` 中添加校验（查找现有代码中的验证位置）：

```go
// 在解析 req.Name 之后，执行数据库操作之前：
if strings.HasPrefix(req.Name, "@") {
    writeJSON(w, http.StatusBadRequest, map[string]string{"error": "collection name cannot start with '@'"})
    return
}
```

在 `handleCollectionRemove` 中添加保护：

```go
if strings.HasPrefix(req.Name, "@") {
    writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot remove system collection"})
    return
}
```

在 `handleCollectionList`（或对应的 `buildCollectionsList`）中过滤 `@` 开头的 collection：

```go
// 在 list_collections 的 MCP handler 中过滤
for _, c := range cols {
    if strings.HasPrefix(c.Name, "@") {
        continue
    }
    // ... append to result
}
```

- [ ] **Step 3: 更新 MCP tool handler**

在 `handleToolCall` 的 switch 中，替换 `memory_add`/`memory_query`/`memory_delete` 的处理：
- `memory_add`: 去掉 `Type` 字段解析
- `memory_query`: 删除整个 case，返回错误提示使用 search
- `memory_delete`: 保持不变
- 新增 `memory_update`:

```go
case "memory_update":
    var req struct {
        ID      int64  `json:"id"`
        Content string `json:"content"`
    }
    if err := convert.FromJsonE(params, &req); err != nil {
        return nil, err
    }
    if req.ID <= 0 {
        return nil, fmt.Errorf("id is required")
    }
    if req.Content == "" {
        return nil, fmt.Errorf("content is required")
    }
    if err := my.memSvc.Update(req.ID, req.Content); err != nil {
        return nil, err
    }
    return map[string]string{"status": "updated"}, nil
```

```go
case "memory_add":
    var req struct {
        Content string `json:"content"`
    }
    if err := convert.FromJsonE(params, &req); err != nil {
        return nil, err
    }
    if req.Content == "" {
        return nil, fmt.Errorf("content is required")
    }
    id, err := my.memSvc.Add(req.Content)
    if err != nil {
        return nil, err
    }
    return map[string]interface{}{"id": id}, nil

case "memory_query":
    return nil, fmt.Errorf("memory_query is removed, use search instead")
```

- [ ] **Step 4: 编译验证**

```bash
go build ./internal/daemon/
```

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/routes.go
git commit -m "feat: rewrite memory handlers, add @-prefix protection for collections"
```

---

### Task 6: 更新 server.go — 调整路由

**Files:**
- Modify: `internal/daemon/server.go`

- [ ] **Step 1: 更新路由表**

替换 `registerRoutes` 中的 routes：

```go
routes := []route{
    {"GET", "/health", (*Daemon).handleHealth},
    {"POST", "/search", (*Daemon).handleSearch},
    {"POST", "/vsearch", (*Daemon).handleVsearch},
    {"POST", "/query", (*Daemon).handleQuery},
    {"POST", "/hyde", (*Daemon).handleHyde},
    {"POST", "/get", (*Daemon).handleGet},
    {"GET", "/status", (*Daemon).handleStatus},
    {"POST", "/collection/add", (*Daemon).handleCollectionAdd},
    {"POST", "/collection/remove", (*Daemon).handleCollectionRemove},
    {"GET", "/collection/list", (*Daemon).handleCollectionList},
    {"POST", "/collection/rename", (*Daemon).handleCollectionRename},
    {"POST", "/rebuild", (*Daemon).handleRebuild},
    {"POST", "/memory/add", (*Daemon).handleMemoryAdd},
    {"POST", "/memory/delete", (*Daemon).handleMemoryDelete},
    {"POST", "/memory/update", (*Daemon).handleMemoryUpdate},
    {"POST", "/mcp", (*Daemon).handleMCP},
}
```

变化：删除 `/memory/query`，新增 `/memory/update`。

- [ ] **Step 2: Commit**

```bash
git add internal/daemon/server.go
git commit -m "feat: remove /memory/query route, add /memory/update route"
```

---

### Task 7: 更新 client.go — 适配 client 方法

**Files:**
- Modify: `internal/daemon/client.go`

- [ ] **Step 1: 更新 client 方法**

删除 `MemoryQuery`，修改 `MemoryAdd`，新增 `MemoryUpdate`：

```go
func (my *Client) MemoryAdd(content string) ([]byte, error) {
    return my.Post("/memory/add", map[string]interface{}{
        "content": content,
    })
}

func (my *Client) MemoryDelete(id int64) ([]byte, error) {
    return my.Post("/memory/delete", map[string]interface{}{
        "id": id,
    })
}

func (my *Client) MemoryUpdate(id int64, content string) ([]byte, error) {
    return my.Post("/memory/update", map[string]interface{}{
        "id":      id,
        "content": content,
    })
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/daemon/client.go
git commit -m "feat: remove MemoryQuery, add MemoryUpdate, simplify MemoryAdd"
```

---

### Task 8: 更新 cli/memory.go — 更新 CLI 命令

**Files:**
- Modify: `internal/cli/memory.go`

- [ ] **Step 1: 重写 CLI 命令**

```go
package cli

import (
    "fmt"

    "github.com/lixianmin/got/convert"
    "github.com/lixianmin/lmd/internal/config"
    "github.com/lixianmin/lmd/internal/daemon"
    "github.com/spf13/cobra"
)

var memoryCmd = &cobra.Command{
    Use:   "memory",
    Short: "Agent memory operations",
}

var memoryAddCmd = &cobra.Command{
    Use:   "add <content>",
    Short: "Add a memory",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        client := daemon.NewClient(config.Cfg.Daemon.Port)
        body, err := client.MemoryAdd(args[0])
        if err != nil {
            return err
        }

        if jsonOut {
            printBody(body)
            return nil
        }

        var resp struct {
            ID        int64  `json:"id"`
            CreatedAt string `json:"created_at"`
        }
        if err := convert.FromJsonE(body, &resp); err != nil {
            return err
        }

        fmt.Printf("id=%d created_at=%s\n", resp.ID, resp.CreatedAt)
        return nil
    },
}

var memoryDeleteCmd = &cobra.Command{
    Use:   "delete <id>",
    Short: "Delete a memory by ID",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        var id int64
        if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil || id <= 0 {
            return fmt.Errorf("invalid id: %s", args[0])
        }

        client := daemon.NewClient(config.Cfg.Daemon.Port)
        body, err := client.MemoryDelete(id)
        if err != nil {
            return err
        }

        if jsonOut {
            printBody(body)
            return nil
        }

        fmt.Printf("Deleted memory id=%d\n", id)
        return nil
    },
}

var memoryUpdateCmd = &cobra.Command{
    Use:   "update <id> <content>",
    Short: "Update a memory by ID",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        var id int64
        if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil || id <= 0 {
            return fmt.Errorf("invalid id: %s", args[0])
        }

        client := daemon.NewClient(config.Cfg.Daemon.Port)
        body, err := client.MemoryUpdate(id, args[1])
        if err != nil {
            return err
        }

        if jsonOut {
            printBody(body)
            return nil
        }

        fmt.Printf("Updated memory id=%d\n", id)
        return nil
    },
}

func init() {
    memoryCmd.AddCommand(memoryAddCmd)
    memoryCmd.AddCommand(memoryDeleteCmd)
    memoryCmd.AddCommand(memoryUpdateCmd)
    rootCmd.AddCommand(memoryCmd)
}
```

删除 `memoryType`、`memoryLimit` 变量、`memoryQueryCmd`，以及 `--type` 和 `--limit` flags。

- [ ] **Step 2: Commit**

```bash
git add internal/cli/memory.go
git commit -m "feat: update CLI: remove query, add update, remove --type flag"
```

---

### Task 9: 更新 MCP server.go — 调整 tool 定义

**Files:**
- Modify: `internal/mcp/server.go`

- [ ] **Step 1: 更新 toolDefs**

```go
var toolDefs = []ToolDef{
    {Name: "search", Description: "Hybrid search (BM25 + vector) across all documents and memories"},
    {Name: "search_lex", Description: "BM25 keyword search"},
    {Name: "search_vector", Description: "Vector semantic search"},
    {Name: "get", Description: "Retrieve document by path or docid"},
    {Name: "status", Description: "Index status"},
    {Name: "list_collections", Description: "List all collections"},
    {Name: "memory_add", Description: "Add a memory (stored as @episodic document)"},
    {Name: "memory_delete", Description: "Delete a memory by id"},
    {Name: "memory_update", Description: "Update a memory by id"},
}
```

变化：删除 `memory_query`，新增 `memory_update`，更新搜索工具描述。

- [ ] **Step 2: Commit**

```bash
git add internal/mcp/server.go
git commit -m "feat: remove memory_query tool, add memory_update tool"
```

---

### Task 10: 运行所有测试，修复 failing tests

**Files:**
- Modify: `internal/dao/memory_test.go`, `internal/service/memory_test.go`
- 可能修改: `internal/service/fusion_test.go`（如果有引用）

- [ ] **Step 1: 重写 dao/memory_test.go**

旧测试依赖 memories/memories_fts/memories_vec 表，需重写为测试 documents+chunks 操作。

```go
package dao

import (
    "os"
    "testing"
)

func TestInsertMemory(t *testing.T) {
    setupTestDB(t)
    id, err := InsertMemory("这是一条测试记忆")
    if err != nil {
        t.Fatalf("InsertMemory: %v", err)
    }
    if id <= 0 {
        t.Fatalf("Expected valid id, got %d", id)
    }

    rec, err := GetMemoryByID(id)
    if err != nil {
        t.Fatalf("GetMemoryByID: %v", err)
    }
    if rec.Content != "这是一条测试记忆" {
        t.Errorf("Expected content '这是一条测试记忆', got %q", rec.Content)
    }
    if rec.Collection != "@episodic" {
        t.Errorf("Expected collection '@episodic', got %q", rec.Collection)
    }
}

func TestDeleteMemory(t *testing.T) {
    setupTestDB(t)
    // 先添加一条记忆
    doc := &DocumentRecord{
        Collection: "@episodic",
        Path:       "/@memory/test-delete",
        Hash:       "test-delete-hash",
        Body:       "待删除的记忆",
    }
    if err := UpsertDocument(doc); err != nil {
        t.Fatalf("UpsertDocument: %v", err)
    }
    // 添加 chunk
    _, err := InsertChunks(doc.Id, []ChunkData{{
        Content: "待删除的记忆", Position: 1, TokenCount: 5, Hash: "test-delete-hash",
    }}, []string{"待删除的记忆"})
    if err != nil {
        t.Fatalf("InsertChunks: %v", err)
    }

    if err := DeleteMemory(doc.Id); err != nil {
        t.Fatalf("DeleteMemory: %v", err)
    }

    _, err = GetMemoryByID(doc.Id)
    if err == nil {
        t.Fatal("Expected error for deleted memory, got nil")
    }
}

func TestUpdateMemory(t *testing.T) {
    setupTestDB(t)
    doc := &DocumentRecord{
        Collection: "@episodic",
        Path:       "/@memory/test-update",
        Hash:       "test-update-old",
        Body:       "旧内容",
    }
    if err := UpsertDocument(doc); err != nil {
        t.Fatalf("UpsertDocument: %v", err)
    }
    _, err := InsertChunks(doc.Id, []ChunkData{{
        Content: "旧内容", Position: 1, TokenCount: 3, Hash: "test-update-old",
    }}, []string{"旧内容"})
    if err != nil {
        t.Fatalf("InsertChunks: %v", err)
    }

    if err := UpdateMemory(doc.Id, "新内容"); err != nil {
        t.Fatalf("UpdateMemory: %v", err)
    }

    rec, err := GetMemoryByID(doc.Id)
    if err != nil {
        t.Fatalf("GetMemoryByID: %v", err)
    }
    if rec.Content != "新内容" {
        t.Errorf("Expected '新内容', got %q", rec.Content)
    }
}

func TestCountMemories(t *testing.T) {
    setupTestDB(t)
    _, err := InsertMemory("记忆1")
    if err != nil {
        t.Fatalf("InsertMemory: %v", err)
    }
    _, err = InsertMemory("记忆2")
    if err != nil {
        t.Fatalf("InsertMemory: %v", err)
    }

    count, err := CountMemories()
    if err != nil {
        t.Fatalf("CountMemories: %v", err)
    }
    if count < 2 {
        t.Errorf("Expected at least 2 memories, got %d", count)
    }
}
```

- [ ] **Step 2: 删除 service/memory_test.go 中的衰减相关测试**

保留 Add/Delete 测试，删除 Query/timePenalty/decay 测试。重写为：

```go
package service

import (
    "testing"

    "github.com/lixianmin/lmd/internal/dao"
)

func TestMemoryServiceAddDelete(t *testing.T) {
    setupServiceTest(t)
    svc := NewMemoryService()

    id, err := svc.Add("测试记忆内容")
    if err != nil {
        t.Fatalf("Add: %v", err)
    }
    if id <= 0 {
        t.Fatalf("Expected valid id, got %d", id)
    }

    if err := svc.Delete(id); err != nil {
        t.Fatalf("Delete: %v", err)
    }
}

func TestMemoryServiceUpdate(t *testing.T) {
    setupServiceTest(t)
    svc := NewMemoryService()

    id, err := svc.Add("旧内容")
    if err != nil {
        t.Fatalf("Add: %v", err)
    }

    if err := svc.Update(id, "新内容"); err != nil {
        t.Fatalf("Update: %v", err)
    }

    rec, err := dao.GetMemoryByID(id)
    if err != nil {
        t.Fatalf("GetMemoryByID: %v", err)
    }
    if rec.Content != "新内容" {
        t.Errorf("Expected '新内容', got %q", rec.Content)
    }
}

func TestMemoryServiceAddEmptyContent(t *testing.T) {
    setupServiceTest(t)
    svc := NewMemoryService()

    _, err := svc.Add("")
    if err == nil {
        t.Fatal("Expected error for empty content")
    }
}
```

- [ ] **Step 3: 运行全部测试**

```bash
go test ./internal/... -count=1 -v 2>&1 | head -100
```

预期：所有测试通过。如有失败，逐一修复。

- [ ] **Step 4: Commit**

```bash
git add internal/dao/memory_test.go internal/service/memory_test.go
git commit -m "test: rewrite memory tests for unified storage"
```

---

### Task 11: 清理和最终验证

**Files:**
- Modify: `docs/01.memory.md`

- [ ] **Step 1: 更新 memory.md**

```
## Memory 层

记忆系统已合并到 documents+chunks 表：
- `@episodic` collection：经历记忆，无时间衰减
- `@knowledge` collection：知识条目（预留）
- memory/add、memory/delete、memory/update 接口保留
- 搜索统一使用 /query 接口，不区分记忆/文档
- `@` 开头的 collection 为系统保留，用户不可创建/删除
```

- [ ] **Step 2: 运行完整构建**

```bash
go build ./...
```

- [ ] **Step 3: 运行完整测试套件**

```bash
go test ./... -count=1
```

预期：全部通过

- [ ] **Step 4: 最终 Commit**

```bash
git add docs/01.memory.md
git commit -m "docs: update memory.md for unified storage design"

git add -A
git diff --cached --stat
# 检查是否还有遗漏
```

