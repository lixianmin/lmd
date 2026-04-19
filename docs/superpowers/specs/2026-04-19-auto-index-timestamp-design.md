# Auto-Index on collection add + Timestamp-based Fast Change Detection

## Problem

Current UX requires 3 separate commands to go from "add a folder" to "search it":

```
lmd collection add /path --name mydocs   → shows "0 docs"
lmd update                               → scans files, builds index
lmd embed                                → generates vectors
```

`collection list` shows `0 docs` after add, which is confusing. The `update` command is a hidden requirement most users won't discover.

Additionally, the indexer uses SHA-256 hash comparison for change detection on every file scan. This means reading every file's full content even when nothing changed. For large collections, this is unnecessarily slow.

## Solution

Two changes:

1. **`collection add` auto-indexes**: After registering the collection, immediately scan and index all files. Output shows doc count directly.

2. **Timestamp-based fast path**: Before computing SHA-256, compare file modification time (`os.Stat().ModTime()`) with stored `file_mod_time`. Only read file content and compute hash when the timestamp differs. This makes `syncIndex()` (called before every search) nearly free when nothing changed.

## Design

### 1. `collection add` Auto-Index

**File**: `internal/cli/collection.go`

After `dao.AddCollection()` succeeds, create an `Indexer` and call `UpdateCollection()` for the new collection:

```
collection add /path --name mydocs
→ Collection 'mydocs' added: /path (15 docs indexed)
```

Error handling: if indexing fails, the collection is still registered (add succeeded) but a warning is printed. The user can run `lmd update` to retry.

**Implementation**: Extract the indexer init logic from `syncIndex()` into a shared helper (or just reuse `service.NewIndexer`). The `collection add` command needs access to `tokenizer.NewGseTokenizer()` and `service.NewIndexer()`.

### 2. Timestamp-based Change Detection

**File**: `internal/dao/schema.go` — add `file_mod_time` column

```sql
ALTER TABLE documents ADD COLUMN file_mod_time INTEGER;
```

`file_mod_time` stores the Unix nanosecond timestamp from `os.Stat().ModTime()`. Using integer (nanoseconds) avoids timezone formatting issues and enables exact comparison.

**File**: `internal/dao/document.go` — add `FileModTime int64` to `DocumentRecord`

`UpsertDocument()` stores the actual file mod time instead of using `DATETIME('now')` for `modified_at`. The `file_mod_time` column is used for fast comparison; `modified_at` remains as "when we last processed this file".

**File**: `internal/service/indexer.go` — two-pass scanning

Current flow:
```
for each file:
    content = ReadFile(path)        ← always reads full content
    hash = SHA256(content)          ← always computes hash
    compare hash with stored hash
```

New flow:
```
for each file:
    stat = os.Stat(path)
    if stored modTime == stat.ModTime:
        skip (unchanged)
        continue
    
    content = ReadFile(path)
    hash = SHA256(content)
    compare hash with stored hash (for correctness)
    store new modTime
```

The mod time comparison is a fast path — `os.Stat()` is a syscall, no file I/O. Only when the timestamp changes do we read the file and compute the hash. The hash comparison is still done as a safety check (e.g., `touch` without content change).

**Edge case**: First-time collection (no stored mod times) — all files are indexed, mod times are stored. This is the `collection add` case.

**ListDocumentsByCollection**: Also return `file_mod_time` so the indexer can compare without an extra query. Change `existingPaths` from `map[string]string` (hash) to `map[string]fileInfo` where `fileInfo` contains both `hash` and `fileModTime`.

### 3. Schema Migration

Add `file_mod_time INTEGER` column to existing `documents` table. Since the project uses no formal migration system (per spec: `CreateTables()` in `schema.go`), add an `ALTER TABLE` in `CreateTables()` wrapped with an "if not exists" check:

```go
// add file_mod_time column if missing
DB.db.Exec("ALTER TABLE documents ADD COLUMN file_mod_time INTEGER DEFAULT 0")
```

This is idempotent — SQLite ignores the error if the column already exists (or we check first).

### 4. Output Changes

| Command | Before | After |
|---------|--------|-------|
| `collection add` | `Collection 'x' added: /path` | `Collection 'x' added: /path (15 docs indexed)` |
| `collection list` | `bin  /path  (0 docs)  **/*.md` | `bin  /path  (15 docs)  **/*.md` (immediately correct) |

## What Does NOT Change

- `update` command remains — used for manual re-sync, e.g. after external changes
- `embed` command remains — `collection add` does NOT auto-embed
- `syncEmbeddings()` stays as-is (max 10 chunks per search)
- `rebuild` command unchanged
- MCP server unchanged

## Testing

1. **Unit test**: `indexer.UpdateCollection()` with mock files — verify mod time fast path skips unchanged files
2. **Unit test**: Verify `collection add` returns indexed count
3. **Integration test**: Add collection → verify `collection list` shows correct doc count immediately
4. **Integration test**: Modify a file's content (preserving mod time via `os.Chtimes`) → verify it's detected as unchanged
5. **Integration test**: Modify a file (normal edit, mod time changes) → verify re-indexed
