# Agents.md
---

## 一：宪法条令：核心原则，严禁违反

### 1 核心文件清单 (必看)

1. Agents.md：
   1. 定义项目核心原则，具有最高优先级，高于任何其它文件或单次会话指令
   2. Agents.md 由人类编辑和维护，禁止 AI 修改，以防止破坏核心原则
   3. 当 Agents.md 有修改时，AI 需跟人类讨论其它受影响文件 (文档，代码) 的修改情况，合并重复，消除冲突。原则上以Agents.md 为准，实在无法调整的，必须在文件中记录理由

2. memory.md: 
   1. 仅包含项目的关键决策或相关文件索引（地图），单条简短清晰（小于200字），禁止包含细节
   2. 类似 ClaudeCode 中技能模板 SKILL.md 中 frontmatter 的 description，帮助 AI 判断事件响应逻辑
   3. memory.md 由 AI 编辑和修改
   4. 位于./docs/01.memory.md


3. todo.md
   1. 待定任务清单
   2. 由人类撰写，AI 发现该文件有变动时，通过 brainstorm 逐条跟人类讨论处理策略
   3. 全部讨论完成后，AI 清空该文件
   4. 位于./docs/02.todo.md



### 2 specs是真理之源

一切架构和代码都服务于 specs (规范) ，而不是反过来。

- specs 位于./docs/superpowers/specs目录下
- 讨论清楚 specs 之前，禁止做任何事情
- specs 变更从 brainstorm 开始，讨论清楚后更新相关文件
- 只实现 specs 中明确要求的功能（YAGNI）



### 3 架构原则

- 单一职责：每个file, class, method, function只做好一件事
- TDD：测试优先，在完成单元测试之间，禁止编写任何实现代码。测试覆盖率: 核心包 ≥ 80%
- 优先使用标准库，第三方库需有充足理由
- 单个方法不超过 40 行
- 参考 The Zen of Python (import this)


---

## 二：编码风格

### 1 数据库

1. redis相关代码位于rdb目录下
2. 其它所有db相关的代码在dao目录下
3. 禁止其它目录出现sql语句

### 2 golang

1. 项目使用vendor，但在.gitignore中将vendor目录加入例外



```go
// 命名规范
prefer `docId` over `docID`

// receiver 统一命名为my
func (my *Flag) AddFlag(flag int64) {
  ...
}

// 使用 loom.Go() 启动 goroutine
import "github.com/lixianmin/got/loom"
loom.Go(func(later loom.Later) {
    ...
})

// 日志库使用：github.com/lixianmin/logo
// 具体使用方式参考项目README.md，示例如下：
logo.Info("连接 %s:%d 成功", host, port)	// Printf 风格（包含 % 格式化动词时自动格式化）
logo.JsonI("sessionId", id, "elapsed", time.Since(start))	// JSON 结构化日志（交替传入 key-value 对）
```
