# Memory 统一存储设计

## 背景

当前 lmd 有两套独立的存储和搜索体系：

| 体系 | 存储 | 搜索 |
|------|------|------|
| 文档 | documents + chunks + chunks_fts + chunks_vec | FTS + Vector → RRF |
| 记忆 | memories + memories_fts + memories_vec | FTS + Vector → RRF → 时间衰减 |

两套体系结构高度相似但相互隔离。作为本地 LLM 服务，需要统一的检索入口。

## 设计目标

1. 记忆与文档统一存储，共用一套检索流水线
2. 类型体系从 Fact/Episode/Relation 简化为 episodic/knowledge
3. 暂不实现时间衰减，先打通存储和检索路径

## 设计

### 1. 用 collection 做类型区分，不加新列

documents 表不变。系统 collection 使用 `@` 前缀标记：

| collection | 含义 | 衰减 |
|------|------|------|
| `@episodic` | 经历记忆 | 暂不衰减 |
| `@knowledge` | 知识条目（预留） | 暂不衰减 |
| 其他（如 `ai`、`notes`） | 文件文档（现有） | 无 |

约束：
- 用户创建 collection 时，名称不允许以 `@` 开头
- `@` 开头的 collection 属于系统保留，不列入 collection 列表，不可通过 collection 删除接口移除

### 2. Schema 变更

**不新增列。仅移除：**
- `memories` 表
- `memories_fts` 虚拟表
- `memories_vec` 虚拟表

**不改动：**
- documents / chunks / chunks_fts / chunks_vec 结构不变
- collections 表不变

### 3. 存储模型

一条记忆 = 1 行 document + 1 行 chunk：

```
记忆写入:
  INSERT INTO documents (docid, collection, path, title, body, hash, ...)
    VALUES ('mem-{hash}', '@episodic', '/@memory/{hash}', '标题', '记忆内容', '<hash>', ...)
  INSERT INTO chunks (doc_id, seq, content, position, hash)
    VALUES (<docId>, 1, '记忆内容', 1, '<hash>')
```

- `docid`：`mem-` 前缀 + 内容 hash 前 12 位，保证唯一且幂等
- `collection`：`@episodic` 或 `@knowledge`，系统内部保留
- `path`：`/@memory/{hash}`，满足 NOT NULL 约束的占位值
- 其他文档特有字段（file_size、modified_at、file_mod_time）对记忆设为 0 或 NULL

document 行持有 metadata，chunk 行负责内容检索（复用 chunks_fts + chunks_vec）。

记忆内容为 LLM 提炼后的短文本，不需要分块，1 条记忆 = 1 个 chunk。

删除只需 `DELETE FROM documents WHERE id = ?`，FK ON DELETE CASCADE 自动清理关联的 chunk。

### 4. 数据迁移

对于已有 memories 表中的数据：

1. 读取全部 memories 行
2. 逐行转为 document（collection=`@episodic`）+ chunk 写入
3. 验证行数一致后，删除 memories、memories_fts、memories_vec 三张表

### 5. 接口

| 接口 | 路由 | MCP tool |
|------|------|------|
| 添加记忆 | POST /memory/add | memory_add |
| 删除记忆 | DELETE /memory/delete | memory_delete |
| 更新记忆 | POST /memory/update | memory_update（新增） |

**查询不设独立接口。** 统一使用现有 query 接口，默认搜所有 collection。查询结果中 `@episodic` 的记忆按 created_at 降序排列（新记忆在前），不应用时间衰减。

### 6. Collection 名称约束

- 用户创建 collection 时校验：名称不得以 `@` 开头
- `@` 开头的 collection 在列表接口中过滤掉，不对外暴露
- collection 删除接口拒绝对 `@` 开头的 collection 操作

## 不涉及

- 不实现时间衰减
- 不实现 Ebbinghaus 遗忘曲线/FSRS 间隔重复
- 不实现 `@knowledge` 的特殊逻辑
- 不改变现有文档同步、分块、embedding 流程

## 涉及文件

| 文件 | 改动 |
|------|------|
| `internal/dao/db_init.go` | 移除 memories 三张表 |
| `internal/dao/memory.go` | 重写为操作 documents + chunks 表 |
| `internal/service/memory.go` | 移除衰减逻辑；适配新的 DAO 接口 |
| `internal/daemon/daemon.go` | 移除 embedMemories；记忆 embed 走 chunk embed 流程 |
| `internal/daemon/routes.go` | 适配 memory handler；collection 增删校验 `@` 前缀 |
| `internal/daemon/client.go` | 适配 memory client |
| `internal/cli/memory.go` | 适配 CLI |
| `internal/mcp/server.go` | 适配 MCP tools；移除 memory_query，统一用 search |
