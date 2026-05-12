# Lazy ScanChanges Design

## Problem

`ScanChanges` reads every changed/new file, hashes it, chunks it, and stores `Body` + `Chunks` in `PendingDoc`. For a collection with 23K files averaging 20KB, peak memory exceeds 1.5GB — all wasted since `processDocNew` processes one file at a time.

Additionally, for very large files (hundreds of MB), `ScanChanges` holds the entire content in memory just to compute hash and chunks.

## Design

Split `ScanChanges` (cheap detection) from `processDocNew` (expensive processing).

### 1. ScanChanges — zero file I/O

Walk files, `os.Stat` only. Change detection uses two signals from stat:

| Condition | Result |
|-----------|--------|
| DB mod_time == file mod_time AND DB file_size == file file_size | Unchanged, skip |
| Either differs | Potentially changed |
| File not found on disk | DocDeleted |
| Not in DB | DocNew |

No file reading, no hashing, no chunking.

### 2. PendingDoc — lightweight

```go
type PendingDoc struct {
    Action      DocAction
    Collection  string
    RootDir     string   // absolute root dir of collection
    Path        string   // relative path within collection
    FileSize    int64
    FileModTime int64
    OldDocId    int64    // set for DocChanged and DocDeleted
}
```

Removed: `Title`, `Body`, `Hash`, `Chunks`.

### 3. ProcessDoc — per-file hash check

```
DocDeleted  → dao.DeleteDocumentAndSummary(oldDocId)

DocChanged  → read file → compute full hash
              → compare with DB hash (old doc still exists)
              → hash same: update mod_time + file_size only, done
              → hash differs: DeleteDocumentAndSummary → processDocNew

DocNew      → processDocNew
```

The hash check before deletion avoids wasted embedding/summarizing API calls for "touch" false positives (mod_time changed but content unchanged).

### 4. processDocNew — full pipeline

1. Read full file content
2. Compute full hash
3. Extract title from content
4. Chunk via chunker
5. Embed chunks in batches of 8
6. Insert chunks + vectors
7. Generate summary via LLM
8. Embed summary
9. Insert summary + vector
10. Complete document (set file_mod_time)

### 5. DB schema — no changes

- `documents.hash` — full file hash, computed in processDocNew
- `documents.file_size` — already exists, now used for change detection
- `documents.file_mod_time` — already exists, now used for change detection

### 6. Memory impact

| Scenario | Before | After |
|----------|--------|-------|
| ScanChanges 23K files | ~1.5GB | ~5MB (stat only) |
| Process one file | Included above | 1 file's content + chunks |
| Process one 100MB file | 100MB in PendingDoc | 100MB in processDocNew (same) |

Single very large files are not solved — the user accepts this limitation.

## Files changed

| File | Change |
|------|--------|
| `internal/service/indexer.go` | `ScanChanges`: remove file reading/hashing/chunking, stat only |
| `internal/service/indexer.go` | `PendingDoc`: remove Body/Title/Hash/Chunks, add RootDir |
| `internal/service/processor.go` | `processDocNew`: add file reading + hashing + chunking |
| `internal/service/processor.go` | `ProcessDoc`: add hash-check-before-delete for DocChanged |
| `internal/dao/document.go` | Add `UpdateFileModTime(docId, modTime, fileSize)` method |
| Tests | Update all ScanChanges/Processor tests for new signatures |
