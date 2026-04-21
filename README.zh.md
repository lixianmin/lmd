**[English Documentation](README.md)**

# LMD - 本地 Markdown 文档搜索引擎

一个 Go 语言编写的本地混合搜索引擎，专注于 Markdown 文档管理，提供一流的中文语言支持。

LMD 结合 **BM25 关键词搜索**（FTS5 + gse 分词）和**向量语义搜索**（sqlite-vec + Qwen3-Embedding），通过 RRF 融合排序和 MMR 多样性重排，提供快速精准的检索能力。后台 daemon 自动管理索引和嵌入，支持 CLI 和 MCP 接口。

## 特性

- **混合搜索**：BM25 + 向量搜索，RRF 融合 + Rocchio PRF 查询扩展 + MMR 多样性重排
- **HyDE 搜索**：通过 SiliconFlow API 生成假设文档，提升召回率
- **中文优先**：gse 分词器提供准确的中文分词能力
- **Markdown 感知**：分块时尊重标题和代码块边界（300 字符目标）
- **Agent 就绪**：MCP Server + JSON 输出，便于 AI Agent 集成
- **Agent 记忆**：支持 fact/episode/relation 类型记忆，带时间衰减评分

## 安装

```bash
git clone https://github.com/lixianmin/lmd.git
cd lmd
make install
```

> **注意：** 需要 CGo 和 C 编译器（GCC/Clang）以支持 SQLite FTS5、sqlite-vec 和 llama-go（嵌入模型）。需先构建 llama-go 子模块：`make submodule`。

## 快速开始

```bash
# 添加文档集合
lmd collection add ~/notes --name mynotes

# 搜索（daemon 自动启动、自动索引、自动嵌入）
lmd search "并发编程"
lmd vsearch "concurrent programming patterns"
lmd query "goroutine channel" -n 10

# HyDE 搜索（需在配置中设置 hyde.api_key）
lmd hyde "Go 如何处理并发"

# 查看文档
lmd get mynotes/go.md
lmd get "#abc123"

# Agent 记忆
lmd memory add "Go 使用 goroutine 实现轻量级并发" --type fact
lmd memory search "并发"

# Daemon 管理
lmd status
lmd stop
lmd rebuild
```

## CLI 命令参考

| 命令 | 说明 |
|------|------|
| `collection add <path> --name <n>` | 添加文档集合 |
| `collection list` | 列出所有集合 |
| `collection remove <name>` | 删除集合 |
| `collection rename <old> <new>` | 重命名集合 |
| `search <query>` | BM25 关键词搜索 |
| `vsearch <query>` | 向量语义搜索 |
| `query <query>` | 混合搜索（BM25 + 向量 + RRF 融合 + MMR） |
| `hyde <query>` | HyDE 搜索（通过假设文档进行向量搜索） |
| `get <collection/path>` 或 `get <#docid>` | 获取文档 |
| `memory add <content> --type <t>` | 添加记忆（fact\|episode\|relation） |
| `memory search <query>` | 搜索记忆 |
| `status` | 查看索引状态 |
| `rebuild` | 清空数据并重建索引 |
| `stop` | 停止 daemon |

### 通用参数（搜索命令）

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--collection, -c` | 全部 | 限定搜索范围 |
| `--limit, -n` | 5 | 返回结果数量 |
| `--full` | false | 显示完整文档内容 |
| `--min-score` | 0 | 最低分数阈值 |
| `--format` | text | 输出格式（text\|md\|csv） |
| `--json` | false | JSON 输出（全局参数） |
| `--verbose` | false | 详细日志（全局参数） |

## 项目结构

```
cmd/lmd/              CLI 入口
internal/cli/         Cobra 命令定义
internal/daemon/      HTTP daemon + 后台索引/嵌入
internal/service/     业务逻辑（索引、搜索、嵌入、记忆）
internal/dao/         SQLite 持久化（FTS5 + sqlite-vec）
internal/embedding/   向量嵌入（llama-go CGo, Qwen3-Embedding-0.6B）
internal/tokenizer/   文本分词（gse）
internal/chunker/     Markdown 感知分块（300 字符，句级重叠）
internal/formatter/   输出格式化（text/json/md/csv）
internal/config/      配置加载（YAML）
internal/mcp/         MCP 协议处理
test/fixtures/        测试文档（中文 + 英文）
```

## 配置

配置文件：`~/.config/lmd/config.yaml`

```yaml
daemon:
  port: 12345

llama:
  embed_model: ~/.cache/lmd/models/Qwen3-Embedding-0.6B-Q8_0.gguf
  gpu_layers: -1
  threads: 4
  parallel: 8
  model_idle_timeout: 10m

embedding:
  batch_size: 8
  truncation: 300

hyde:
  base_url: https://api.siliconflow.cn/v1
  api_key: ""
  model: Qwen/Qwen3.5-9B
  max_tokens: 200

database:
  path: ~/.cache/lmd/index.sqlite
```

## 技术栈

| 组件 | 库 | 用途 |
|------|-----|------|
| CLI | [cobra](https://github.com/spf13/cobra) | 命令框架 |
| SQLite | [go-sqlite3](https://github.com/mattn/go-sqlite3) | 数据库（WAL 模式） |
| 全文搜索 | FTS5 + [gse](https://github.com/go-ego/gse) | BM25 + 中文分词 |
| 向量搜索 | [sqlite-vec](https://github.com/asg017/sqlite-vec) | KNN 余弦相似度（1024 维） |
| 嵌入模型 | [llama-go](https://github.com/tcpipuk/llama-go) + Qwen3-Embedding-0.6B | 本地向量嵌入（Metal GPU） |
| HyDE | SiliconFlow API（OpenAI 兼容） | 假设文档生成 |

## 开发

```bash
make build          # 编译
make test           # 运行测试
make test-verbose   # 运行测试（详细输出）
make vet            # 静态分析
make lint           # vet + fmt
make e2e            # 端到端测试
make all            # lint + test + build
make clean          # 清理编译产物
```

## 许可证

MIT
