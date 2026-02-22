# 第三章：让 AI 自主奔跑——Agent 循环与智能摘要

> 工具有了，对话有了，但真正的 Agent 不是问一次答一次——
> 它需要持续思考，自主完成复杂任务，还要聪明地管理自己的"记忆空间"。

---

## System Prompt：写给 AI 的"员工手册"

在 Agent 开始工作之前，它需要一份"员工手册"——system prompt。这不只是几行描述，而是 AI 行为的完整规范。

好的 system prompt 决定了 AI 是一个得力助手还是一个乱干一气的新手。

```go
// 伪码：动态 system prompt 构建
// 对应真实代码：internal/agent/agent.go:New()

func buildSystemPrompt(basePrompt, workspace string) string {
    // 1. 加载自定义基础提示（来自文件或配置）
    prompt := basePrompt

    // 2. 注入技能元数据（第7章详讲）
    if skillsMetadata != "" {
        prompt += "\n\n" + skillsMetadata
    }

    // 3. 注入 Todo 工具使用指南
    prompt += "\n\n" + TodoPrompt

    // 4. 注入运行时环境信息
    prompt += fmt.Sprintf(`
## Current Environment
**Current Date/Time**: %s (%s)
**Operating System**: %s
**Workspace Directory**: %s

All relative paths will be resolved relative to the workspace directory.
This system uses a cross-platform POSIX shell (bash tool).
    `, time.Now().Format(...), osInfo.Name, workspace)

    return prompt
}
```

**为什么要注入日期和 OS 信息？**

LLM 不知道当前时间（训练数据有截止日期），也不知道运行在 Windows 还是 Linux。注入这些信息后，AI 就会自动使用正确的命令语法——Windows 上不会建议用 `ls -la`，而会建议用 `dir`。

---

## Agent 主循环：六步理解

这是整个框架的心脏——`runLoop` 函数：

```go
// 伪码：Agent 主循环（简化版）
// 对应真实代码：internal/agent/agent.go:runLoop()

func (a *Agent) runLoop(ctx, prompt, callback) (string, error) {
    // 第 0 步：把用户消息加入对话历史
    a.messages.add(UserMessage(prompt))

    for step := 0; step < a.maxSteps; step++ {

        // 第 1 步：检查是否需要压缩历史（Token 管理）
        a.maybeSummarize(ctx, step)

        // 第 2 步：调用 LLM（带工具定义）
        toolSchemas := a.toolRegistry.GetSchemas()
        response, err := a.model.GenerateStream(ctx, a.messages, toolSchemas, callback)
        if err != nil {
            return "", err
        }

        // 第 3 步：更新 Token 计数
        a.lastInputTokens = response.Usage.InputTokens

        // 第 4 步：把 AI 回复加入对话历史
        a.messages.add(AssistantMessage(response))

        // 第 5 步：如果没有工具调用，任务完成！
        if len(response.ToolCalls) == 0 {
            return response.Content, nil  // ← 正常结束点
        }

        // 第 6 步：执行工具调用，结果加入对话历史
        for _, toolCall := range response.ToolCalls {
            result := a.executeTool(ctx, toolCall)
            a.messages.add(ToolMessage(toolCall.Name, result))
        }
        // 然后回到第 1 步，继续循环
    }

    return "", fmt.Errorf("超过最大步骤数 %d", a.maxSteps)
}
```

**关键逻辑**：
- `len(response.ToolCalls) == 0` → 任务完成，返回结果
- 否则 → 执行工具，把结果加回对话，继续循环
- 超过 `maxSteps`（默认 200）→ 强制中止，防止无限循环

**消息流的完整形态**：

```
messages = [
    System("你是一个 AI 助手..."),           # 始终保留
    User("帮我分析项目结构"),                 # 用户输入
    Assistant("好的，我先看看目录" + [ls()]),  # AI 决定用工具
    Tool("ls", "cmd/ internal/ go.mod"),     # 工具结果
    Assistant("再读一个文件" + [read(...)]),  # AI 继续
    Tool("read", "文件内容..."),              # 工具结果
    Assistant("分析完成：..."),               # 无工具调用 → 结束
]
```

---

## 工具执行：executeTool 细节

```go
// 伪码：工具执行
// 对应真实代码：internal/agent/agent.go:executeTool()

func (a *Agent) executeTool(ctx context.Context, toolCall ToolCall) (*ToolResult, error) {
    // 查找工具
    tool, exists := a.toolRegistry.Get(toolCall.Function.Name)
    if !exists {
        // 工具不存在：返回友好错误（不崩溃）
        errorMsg := fmt.Sprintf(
            "Unknown tool '%s'. Available tools: %v",
            toolCall.Function.Name,
            a.GetToolNames(),
        )
        return ErrorResult(errorMsg), nil
    }

    // 执行工具
    result, err := tool.Execute(ctx, toolCall.Function.Arguments)
    if err != nil {
        result = ErrorResult(fmt.Sprintf("Tool execution failed: %v", err))
    }

    // 记录日志（JSONL 格式写入 .agent/logs/）
    a.logger.LogToolResult(toolCall.Function.Name, toolCall.Function.Arguments, result)

    // 工具结果作为新消息加入对话
    a.messages.add(ToolMessage(toolCall.Function.Name, result.Content, toolCall.ID))

    return result, nil
}
```

**工具不存在时不崩溃**——这很重要。AI 有时会"幻想"一个不存在的工具名称（比如把 `bash` 写成 `execute`）。我们把错误作为工具结果返回给 LLM，让它自己纠正，而不是让整个 Agent 崩溃。

---

## Token 危机：为什么会超限？

每个 LLM 都有 Context Window 限制（Claude Sonnet 是 200K tokens）。随着对话轮数增加，消息列表越来越长：

```
round 1:   1,200 tokens
round 5:   5,800 tokens
round 20: 23,000 tokens
round 60: 68,000 tokens  ← 接近危险区
round 80: 91,000 tokens  ← 超限！报错！
```

一旦超限，LLM 会拒绝请求，Agent 就挂了。

---

## Pi 风格智能摘要：切点算法

项目实现了一套参考 Pi(pi-core项目) 设计的智能摘要策略：

```
处理前（超出 Token 限制）：
┌─────────────────────────────────────────────────────┐
│ System │ User1│ Ast1 │ Tool│ Tool │ User2│ Ast2 │... │
│ Prompt │      │      │  1  │  2   │      │      │    │
└─────────────────────────────────────────────────────┘
  ▲                                           ▲
  保留                                    cut点之后保留(20K)

处理后（压缩历史，保留近期）：
┌──────────────────────────────────────────────────────┐
│ System │ [Compaction │ User2│ Ast2 │ Tool │ ...最近  │
│ Prompt │  Summary]   │      │      │  N   │  20K     │
└──────────────────────────────────────────────────────┘
```

**算法步骤**：

```go
// 伪码：切点算法
// 对应真实代码：internal/agent/agent.go:maybeSummarize()

func (a *Agent) maybeSummarize(ctx context.Context, currentStep int) error {
    // 检查 token 数量
    if a.lastInputTokens <= a.tokenLimit {
        return nil  // 还没到限制
    }

    // 从末尾向前累积 token，找到 20K 边界
    cutIdx := -1
    tailTokens := 0
    for i := len(a.messages) - 1; i > 0; i-- {
        tailTokens += estimateTokens(a.messages[i])
        if tailTokens >= keepRecentTokens {  // keepRecentTokens = 20000
            // 找到安全切点（必须是 user 或 assistant 消息的起点）
            // 不能切在 tool call / tool result 的中间！
            for j := i; j < len(a.messages); j++ {
                if a.messages[j].IsUser() || a.messages[j].IsAssistant() {
                    cutIdx = j
                    break
                }
            }
            break
        }
    }

    // 压缩 [1:cutIdx] 区间的消息（保留 [0] 系统提示）
    toCompress := a.messages[1:cutIdx]
    summary, err := a.createSummary(ctx, toCompress)

    // 替换为：System + [Summary as User] + Recent
    a.messages = [System] + [UserMessage("[Compaction Summary]\n" + summary)] + a.messages[cutIdx:]
}
```

**为什么不能切在 tool call 中间？**

tool call 和 tool result 是配对的。如果切了 tool call 但保留了 tool result，LLM 会困惑——这个结果是哪个调用返回的？所以必须找到安全的切点。

---

## 摘要内容：结构化检查点

压缩不是简单删除，而是让 LLM 生成一份结构化摘要：

```
[Compaction Summary]

## Goal
用户希望重构 internal/tools/ 目录，将所有文件工具合并成一个文件

## Progress
- 已读取 ls_tool.go, read_tool.go, write_tool.go（共 3 个文件）
- 已确认所有工具都基于 BaseTool 结构
- 已创建 file_tools.go，合并了 ls 和 read 工具

## Key Decisions
- 保留各工具独立的 struct，不合并成一个大 struct
- 使用 init() 而不是工厂函数注册工具

## Current State
正在处理 write_tool.go，准备将其内容合并进 file_tools.go

## Next Steps
- 合并 write_tool.go 和 edit_tool.go
- 删除原始独立文件
- 更新 agent.go 中的注册调用

## Files Touched
- read: ls_tool.go, read_tool.go, write_tool.go
- write: file_tools.go
```

**增量更新**：如果已有摘要，下次压缩时使用 UPDATE 模式，而不是重头生成——这样摘要会随着工作推进而不断完善，而不是每次都忘记之前发生了什么。

---

## 多轮工具调用：模拟演示

下面展示一个真实的多轮 Agent 执行过程：

```
你: 帮我分析项目代码结构，生成一份 report.md

Step 1 ──────────────────────────────────────────
AI 思考: 我需要先了解项目结构
AI 调用: ls(path=".")
工具结果: cmd/ internal/ go.mod go.sum README.md

Step 2 ──────────────────────────────────────────
AI 思考: 看看 internal 里有什么
AI 调用: ls(path="internal")
工具结果: agent/ llm/ tools/ gateway/ task/ schema/ config/

Step 3 ──────────────────────────────────────────
AI 思考: 我应该读一下 go.mod 了解依赖
AI 调用: read(file_path="go.mod")
工具结果: module mini-agent-go\ngo 1.21\nrequire (...)

Step 4 ──────────────────────────────────────────
AI 思考: 信息足够了，开始写报告
AI 调用: write(file_path="report.md", content="# 项目分析报告\n...")
工具结果: 文件写入成功

Step 5 ──────────────────────────────────────────
AI 回复: (无工具调用 → 循环结束)
"分析完成！report.md 已生成。

项目是一个 Go AI Agent 框架，包含：
- 6 个核心包（agent, llm, tools, gateway, task, schema）
- 主要依赖：Anthropic SDK、mvdan/sh（Shell 解释器）
- 架构清晰，模块间低耦合

详见 report.md。"
```

全程 AI 自主完成，没有人工干预。

---

## 日志系统

所有 LLM 请求和工具执行都被记录到 `.agent/logs/` 目录：

```
.agent/logs/
├── mini-agent-20260222-143022.jsonl   # 每次运行一个文件
└── gateway_20260222_140015.log        # Gateway 日志
```

每行是一个 JSON 对象（JSON Lines 格式）：

```json
{"type": "request", "timestamp": "...", "messages": [...], "tools": [...]}
{"type": "response", "timestamp": "...", "content": "...", "tool_calls": [...]}
{"type": "tool_result", "timestamp": "...", "tool": "bash", "args": {...}, "result": {...}}
```

这让你可以精确回放 Agent 的每一个决策，非常有利于调试和审计。

---

## 本章小结

本章完成了 Agent 的"大脑"：

- **System Prompt**：动态注入 OS、时间、技能元数据，AI 知道自己在什么环境里工作
- **主循环**：`UserMessage → LLM → 有工具调用 → 执行 → 回传 → 继续 / 无工具调用 → 返回`
- **工具执行**：容错处理（工具不存在时返回友好错误，不崩溃）
- **Token 管理**：Pi 风格切点算法 + 结构化摘要，永远不会因为"记太多"而崩溃
- **日志系统**：JSONL 格式完整记录每次 LLM 调用和工具执行

对应代码：
- `internal/agent/agent.go` — 完整的 Agent 实现
- `internal/agent/message.go` — 消息类型定义
- `internal/agent/callback.go` — 流式回调适配器
- `internal/utils/logger.go` — 日志系统

---

## 下一章预告

Agent 跑起来了，但你不可能一直盯着终端。这章我们给它装上"驾驶舱"——终端 TUI、远程 Web 控制、手机通知、IM 机器人……

**[第四章：全渠道驾驶舱——TUI/Web/通知/IM](ch04-interface.md)**
