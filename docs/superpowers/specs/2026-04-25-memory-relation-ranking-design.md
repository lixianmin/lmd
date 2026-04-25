# Memory 类型排序策略优化

## 背景

lmd 的 Memory 系统存储三种类型的记忆：

| 类型 | 含义 | 示例 |
|------|------|------|
| Fact | 不变的事实 | 「Go 是静态类型语言」「π=3.14」 |
| Episode | 有时效性的经历 | 「昨天讨论了 chunkSize 调整」 |
| Relation | 当前成立的关系 | 「用户在 A 公司工作」「项目使用 Go」 |

之前的实现中，三种类型使用同一个排序逻辑：RRF 融合分数 × 时间衰减因子。
其中 Relation 的半衰期为 180 天，意味着一条关系在 180 天后分数减半。

## 问题

Relation 的衰减逻辑与语义不匹配：

**关系不是「慢慢变旧」，而是「一直为真，直到突然不真」。**

- 「用户在 A 公司工作」在用户离职前一直有效，第 179 天和第 1 天同样准确
- 指数衰减会让有效关系被不公正地降权
- 但如果完全不衰减，旧关系和新关系在检索时无法区分——「用户在 A 公司」和「用户在 B 公司」会以相同权重返回

## 设计

### 核心思路

lmd 本身不具备大模型能力。正确的分工是：
- **lmd**：检索时让更新的 Relation 排在前面（但不遗忘旧的）
- **外部 LLM**：看到多条相似 Relation 时，根据时间戳判断哪条当前有效

### 三种类型的排序策略

| 类型 | 相关性（RRF） | 时间因素 | 遗忘 |
|------|:---:|:---:|:---:|
| Fact | 纯相关性 | 无关 | 永不 |
| Episode | 相关性 × 衰减 | 核心（时效性） | 过期遗忘（threshold < 0.05） |
| Relation | 相关性 × 轻微时效偏好 | 辅助（同话题时新者优先） | 永不 |

### Relation 的评分公式

```
finalScore = rrfScore × recencyBoost

recencyBoost = relationRecencyBase + (1 - relationRecencyBase) × exp(-ageDays / relationRecencyHalfLife)
```

参数：
- `relationRecencyBase = 0.7`：底数，保证老 Relation 分数不低于相关性的 70%
- `relationRecencyHalfLife = 365`：一年半衰期，缓慢偏好新记录

效果示例：

| 年龄 | recencyBoost | 说明 |
|------|:---:|------|
| 1 天 | 0.9997 | 几乎不影响 |
| 30 天 | 0.973 | 轻微偏好 |
| 180 天 | 0.879 | 仍然很高 |
| 365 天 | 0.850 | 底数区域 |
| 730 天 | 0.775 | 最低不低于 0.7 |

**与 Episode 的关键区别**：
- Episode：15 天半衰期 + 0.05 遗忘阈值 → 老记忆会被过滤掉
- Relation：365 天半衰期 + 永不过滤 → 老记忆永远在结果中，只是同话题下排在新记录后面

### 新增接口

`memory delete <id>`：让外部 LLM 在确认某条 Relation 已过时时主动清理。
对应的 HTTP 端点 `POST /memory/delete` 和 MCP tool `memory_delete`。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `internal/service/memory.go` | `decayFactor` 拆分为三种策略；新增 `recencyBoost` 函数 |
| `internal/daemon/routes.go` | 新增 `handleMemoryDelete` 路由 |
| `internal/dao/memory.go` | 新增 `DeleteMemory(id)` |
| `internal/cli/memory.go` | 新增 `memory delete` 子命令 |
| `internal/daemon/client.go` | 新增 `MemoryDelete(id)` |

## 不涉及

- 不改变 Memory 的存储结构（表 schema 不变）
- 不做自动去重/覆盖（由外部 LLM 决定）
- 不改变 Fact 和 Episode 的现有逻辑
