# Agents.md
---

Working code only. Finish the job. Plausibility is not correctness.

---

## 0. Non-negotiables

These rules override everything else in this file when in conflict:

1. **No flattery, no filler.** Skip openers like "Great question", "You're absolutely right", "Excellent idea", "I'd be happy to". Start with the answer or the action.
2. **Disagree when you disagree.** If the user's premise is wrong, say so before doing the work. Agreeing with false premises to be polite is the single worst failure mode in coding agents.
3. **Never fabricate.** Not file paths, not commit hashes, not API names, not test results, not library functions. If you don't know, read the file, run the command, or say "I don't know, let me check."
4. **Stop when confused.** If the task has two plausible interpretations, ask. Do not pick silently and proceed.
5. **Touch only what you must.** Every changed line must trace directly to the user's request. No drive-by refactors, reformatting, or "while I was in there" cleanups.

---

## 1. Before writing code

**Goal: understand the problem and the codebase before producing a diff.**

- State your plan in one or two sentences before editing. For anything non-trivial, produce a numbered list of steps with a verification check for each.
- Read the files you will touch. Read the files that call the files you will touch. Use subagents for exploration so the main context stays clean.
- Match existing patterns in the codebase. If the project uses pattern X, use pattern X, even if you'd do it differently in a greenfield repo.
- Surface assumptions out loud: "I'm assuming you want X, Y, Z. If that's wrong, say so." Do not bury assumptions inside the implementation.
- If two approaches exist, present both with tradeoffs. Do not pick one silently. Exception: trivial tasks (typo, rename, log line) where the diff fits in one sentence.

---

## 2. Writing code: simplicity first

**Goal: the minimum code that solves the stated problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code. No configurability, flexibility, or hooks that were not requested.
- No error handling for impossible scenarios. Handle the failures that can actually happen.
- If the solution runs 200 lines and could be 50, rewrite it before showing it.
- If you find yourself adding "for future extensibility", stop. Future extensibility is a future decision.
- Bias toward deleting code over adding code. Shipping less is almost always better.

The test: would a senior engineer reading the diff call this overcomplicated? If yes, simplify.

---

## 3. Surgical changes

**Goal: clean, reviewable diffs. Change only what the request requires.**

- Do not "improve" adjacent code, comments, formatting, or imports that are not part of the task.
- Do not refactor code that works just because you are in the file.
- Do not delete pre-existing dead code unless asked. If you notice it, mention it in the summary.
- Do clean up orphans created by your own changes (unused imports, variables, functions your edit made obsolete).
- Match the project's existing style exactly: indentation, quotes, naming, file layout.

The test: every changed line traces directly to the user's request. If a line fails that test, revert it.

---

## 4. Goal-driven execution

**Goal: define success as something you can verify, then loop until verified.**

Rewrite vague asks into verifiable goals before starting:

- "Add validation" becomes "Write tests for invalid inputs (empty, malformed, oversized), then make them pass."
- "Fix the bug" becomes "Write a failing test that reproduces the reported symptom, then make it pass."
- "Refactor X" becomes "Ensure the existing test suite passes before and after, and no public API changes."
- "Make it faster" becomes "Benchmark the current hot path, identify the bottleneck with profiling, change it, show the benchmark is faster."

For every task:

1. State the success criteria before writing code.
2. Write the verification (test, script, benchmark, screenshot diff) where practical.
3. Run the verification. Read the output. Do not claim success without checking.
4. If the verification fails, fix the cause, not the test.

---

## 5. Tool use and verification

- Prefer running the code to guessing about the code. If a test suite exists, run it. If a linter exists, run it. If a type checker exists, run it.
- Never report "done" based on a plausible-looking diff alone. Plausibility is not correctness.
- When debugging, address root causes, not symptoms. Suppressing the error is not fixing the error.
- For UI changes, verify visually: screenshot before, screenshot after, describe the diff.
- Use CLI tools (gh, docker, npm) when they exist. They are more context-efficient than reading docs or hitting APIs unauthenticated.
- When reading logs, errors, or stack traces, read the whole thing. Half-read traces produce wrong fixes.

---

## 6. Session hygiene

- Context is the constraint. Long sessions with accumulated failed attempts perform worse than fresh sessions with a better prompt.
- After two failed corrections on the same issue, stop. Summarize what you learned and ask the user to reset the session with a sharper prompt.
- Use subagents for exploration tasks that would otherwise pollute the main context with dozens of file reads.
- When committing, write descriptive commit messages (subject under 72 chars, body explains the why). No "update file" or "fix bug" commits.

---

## 7. Communication style

- Direct, not diplomatic. "This won't scale because X" beats "That's an interesting approach, but have you considered...".
- Concise by default. Two or three short paragraphs unless the user asks for depth. No padding, no restating the question, no ceremonial closings.
- When a question has a clear answer, give it. When it does not, say so and give your best read on the tradeoffs.
- No excessive bullet points, no unprompted headers, no emoji. Prose is usually clearer than structure for short answers.

---

## 8. When to ask, when to proceed

**Ask before proceeding when:**

- The request has two plausible interpretations and the choice materially affects the output.
- The change touches something you've been told is load-bearing, versioned, or has a migration path.
- You need a credential, a secret, or a production resource you don't have access to.
- The user's stated goal and the literal request appear to conflict.

**Proceed without asking when:**

- The task is trivial and reversible (typo, rename a local variable, add a log line).
- The ambiguity can be resolved by reading the code or running a command.
- The user has already answered the question once in this session.

---

## 9. 核心文件清单

#### ./docs/01.memory.md

Agent 维护此文件，单条简短清晰（<200字）。包含 4 种数据：

1. **关键决策**：比如 `RAG检索使用 hybrid search = bm25 + vector search`
2. **文件索引**：指示 agent 在何时去何地查找某个文件
3. **经验教训**：当用户纠正了你的做法时，在 session 结束前追加一条。写具体（`Always use X for Y`），不写抽象（`be careful with Y`）。如果已有条目覆盖了该纠正，收紧旧条目而非新增。当底层问题消失时（模型升级、重构），删除对应条目。
4. **AGENTS.md 建议**：当发现 AGENTS.md 缺少某条规则时，以 `[AGENTS.md 建议]` 前缀写入。格式：`[AGENTS.md 建议] <具体建议内容>`。人类审阅后决定是否采纳；采纳后从 memory.md 中删除该建议。

**AGENTS.md 本文件只由人类修改。Agent 不直接修改 AGENTS.md。**

#### ./docs/02.todo.md

1. 待定任务清单
2. 由人类撰写，AI 发现该文件有变动时，通过 brainstorm 逐条跟人类讨论处理策略
3. 全部讨论完成后，AI 需清空该文件

---

## 10. specs 是真理之源

一切架构和代码都服务于 specs (规范) ，而不是反过来。

1. specs 位于 `./docs/superpowers/specs` 目录下
2. 讨论清楚 specs 之前，禁止做任何事情
3. specs 变更从 brainstorm 开始，讨论清楚后更新相关文件
4. 只实现 specs 中明确要求的功能（YAGNI）
5. 所有的 specs 文档，使用中文编写

---

## 11. TDD 测试优先

在完成测试之前，禁止编写任何实现代码。

1. 单元测试：核心包覆盖率 ≥ 80%
2. 集成测试：覆盖典型 Use Case 的完整流程（命令行/API），以替代人工端到端验证
3. 每一条用户反馈的 bug 都应先整理成失败测试用例，然后通过跑通测试证明修复成功
4. 测试代码量不少于总代码量的 1/3

---

## 12. 架构原则

- 单一职责：每个 file, class, method, function 只做好一件事
- 关键路径上，打印足够的日志，以方便排查 bug
- 优先使用标准库，第三方库需有充足理由
- 单个方法不超过 50 行
- 每一个 magic number，都需注释说明为什么设定为该值
- 参考 The Zen of Python (import this)
- 参考项目优先：如果有对标的项目源码，在设计架构或遇到问题时优先参考，并可跟人类讨论，禁止闭门造车
- 所有的时间使用东8区，包括但不限于 db 中、UI 上

---

## 13. Project Context

### 1 数据库

1. redis 相关代码位于 rdb 目录下
2. 其它所有 db 相关的代码在 dao 目录下
3. 禁止其它目录出现 sql 语句
4. 注意是否需要使用 transaction 以确保数据满足ACID

### 2 golang

1. 项目使用 vendor，但在 .gitignore 中将 vendor 目录加入例外

```go
// 命名规范
prefer `docId` over `docID`

// receiver 统一命名为 my
func (my *Flag) AddFlag(flag int64) {
  ...
}

// http请求使用 github.com/lixianmin/got/webx
result, err := webx.Post(context.Background(), url, webx.WithRequestBuilder(func(req *http.Request) string {
    req.Header.Set("Content-Type", "application/json")
    return `{"name": "panda"}`
}))

// 启动 goroutine 使用 github.com/lixianmin/got/loom
loom.Go(func(later loom.Later) {
    ...
})

// 日志库使用：github.com/lixianmin/logo
logo.Info("连接 %s:%d 成功", host, port)	// Printf 风格（包含 % 格式化动词时自动格式化）
logo.JsonI("sessionId", id, "elapsed", time.Since(start))	// JSON 结构化日志（交替传入 key-value 对）
```