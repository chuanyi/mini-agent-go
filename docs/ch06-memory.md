# 第六章：记忆宫殿——持久内存与心跳监控

> 真正了解你的助手会记住你的偏好、习惯、重要信息。
> 这章给数字员工建一座"记忆宫殿"——它不再每次都是一张白纸。

---

## 三种记忆层次

数字员工有三种不同性质的"记忆"：

```
┌─────────────────────────────────────────────────────────┐
│               记忆层次                                   │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  层次 1：对话历史（临时工作记忆）                        │
│  ──────────────────────────────                         │
│  存在于：内存中的 messages 数组                          │
│  生命周期：当次会话                                      │
│  容量：受 Context Window 限制（触发摘要后压缩）           │
│  用途：当前任务的上下文                                  │
│                                                         │
│  层次 2：持久笔记（长期记忆）                            │
│  ──────────────────────────────                         │
│  存在于：.agent/memory.json                             │
│  生命周期：永久（除非主动删除）                          │
│  容量：无限（JSON 文件）                                 │
│  用途：跨会话的偏好、知识、记录                          │
│                                                         │
│  层次 3：Token 摘要（工作记忆延续）                      │
│  ──────────────────────────────                         │
│  存在于：messages[1]（系统提示之后）                     │
│  生命周期：当次会话（但跨越多轮摘要）                    │
│  容量：1-2K tokens（结构化摘要）                        │
│  用途：长会话中保留"做过什么"的记忆                     │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

---

## 持久笔记：record_note 和 recall_note

这是最直接的跨会话记忆机制：

### 存储结构

```json
// .agent/memory.json
{
  "notes": [
    {
      "id": "note-001",
      "content": "用户偏好：代码注释使用英文",
      "timestamp": "2026-02-22T10:30:00Z"
    },
    {
      "id": "note-002",
      "content": "项目约定：所有 Go 错误必须使用 %w 包裹",
      "timestamp": "2026-02-22T11:15:00Z"
    },
    {
      "id": "note-003",
      "content": "Server 酱 Key：SCT12345...",
      "timestamp": "2026-02-20T09:00:00Z"
    }
  ]
}
```

### record_note 工具

```go
// 伪码：record_note 工具
// 对应真实代码：internal/tools/note_tool.go

type RecordNoteTool struct {
    BaseTool
    memoryFile string  // .agent/memory.json 路径
}

func (t *RecordNoteTool) Execute(ctx, params) (*ToolResult, error) {
    content := params["content"].(string)

    // 加载现有笔记
    notes := loadNotes(t.memoryFile)

    // 添加新笔记
    note := Note{
        ID:        generateID(),
        Content:   content,
        Timestamp: time.Now(),
    }
    notes = append(notes, note)

    // 写回文件
    saveNotes(t.memoryFile, notes)

    return ContentResult(fmt.Sprintf("已记录，ID: %s", note.ID)), nil
}
```

### recall_note 工具

```go
// 伪码：recall_note 工具

func (t *RecallNoteTool) Execute(ctx, params) (*ToolResult, error) {
    query := OptionalString(params, "query", "")  // 搜索关键词（可选）

    notes := loadNotes(t.memoryFile)

    if query == "" {
        // 无关键词：返回所有笔记
        return ContentResult(formatAll(notes)), nil
    }

    // 关键词过滤（简单的字符串包含匹配）
    var matched []Note
    for _, note := range notes {
        if strings.Contains(strings.ToLower(note.Content), strings.ToLower(query)) {
            matched = append(matched, note)
        }
    }

    return ContentResult(formatNotes(matched)), nil
}
```

---

## 记忆的实际使用场景

### 场景一：记住用户偏好

```
[第一次会话]

你: 记住，我写代码时喜欢把错误处理放在函数最后，而不是 early return

AI: [调用 record_note]
    content: "用户代码风格偏好：错误处理放在函数最后，避免 early return 模式"
    好的，已记录你的代码风格偏好。

[几天后，新会话]

你: 帮我优化一下这个函数

AI: [调用 recall_note, query: "代码风格"]
    [读取到: 错误处理放在函数最后，避免 early return]

AI: 好的，我按照你的代码风格偏好来优化，
    错误处理统一放在函数末尾...
```

### 场景二：跨会话知识积累

```
[多次会话后，memory.json 中积累了：]

- "项目编译命令：go build -o mini-agent-go ./cmd"
- "测试命令：go test ./... -v"
- "部署服务器：192.168.1.100，SSH 端口 2222"
- "代码审查标准：文件 docs/review-checklist.md"
- "Git 提交规范：使用英文，feat/fix/docs 前缀"
```

这些知识无需每次重新告诉 Agent，它会在需要时用 `recall_note` 查找。

---

## Token 摘要：工作记忆的延续

第三章已经介绍了摘要算法的技术细节。这里从"记忆"角度重新理解它：

摘要本质上是把**短期工作记忆**（丰富但大量消耗 Token 的对话历史）压缩成**中期工作记忆**（结构化但节省 Token 的摘要），而长期记忆（record_note）则完全不占用 Context Window。

```
长会话记忆演化（以一个 4 小时重构任务为例）：

小时 1：
messages = [System, User1, Ast1, Tool×5, Ast2, Tool×3, ...]
token 数：约 15,000

小时 2：达到摘要阈值
messages = [System, [Summary1: Goal/Progress/State], User_recent...]
token 数：重置到约 5,000

小时 3：新内容累积
messages = [System, [Summary1], User2, Ast, Tool×8, ...]
token 数：约 18,000

小时 4：再次压缩（增量更新 Summary1）
messages = [System, [Summary2: 更新版本], User_recent...]
token 数：再次重置
```

关键是摘要会**增量更新**——Summary2 是在 Summary1 的基础上合并新进展，而不是完全重写。AI 的"记忆"随着工作推进而不断完善。

---

## 摘要结构：结构化检查点

来看一个真实任务场景中的摘要内容：

```markdown
[Compaction Summary]

## Goal
重构 internal/tools/ 目录：将 12 个独立工具文件合并为 4 个分组文件，
提升代码组织性。

## Progress
- ✅ 分析了现有 12 个工具文件的结构
- ✅ 设计了 4 个分组方案（file_tools, bash_tools, web_tools, note_tools）
- ✅ 合并完成：file_tools.go（包含 ls/read/write/edit/multiedit）
- ✅ 合并完成：bash_tools.go（包含 bash/bash_output/bash_kill）
- 🔄 进行中：web_tools.go

## Key Decisions
- 各工具保留独立 struct，不合并为一个大 struct（保持可读性）
- 使用 NewXxxTool() 工厂函数而非 init() 注册（方便测试）
- 路径安全检查统一在 BaseTool.ResolvePath() 中

## Current State
正在将 web_fetch_tool.go 和 web_download_tool.go 合并到 web_tools.go

## Next Steps
- [ ] 完成 web_tools.go
- [ ] 合并 note_tool.go 到 note_tools.go
- [ ] 更新 agent.go 中的 RegisterDefaultTools() 调用
- [ ] 删除原始的 12 个独立文件
- [ ] 运行 go build 验证编译通过

## Files Touched
- read: ls_tool.go, read_tool.go, write_tool.go, edit_tool.go, multiedit_tool.go
- write: file_tools.go, bash_tools.go
- edit: agent.go (RegisterDefaultTools 更新)
```

这份摘要让新的 LLM 调用能够准确知道：做了什么、决定了什么、下一步干什么。即使对话历史被压缩，任务也能无缝继续。

---

## 心跳与监控

对于长期运行的 Agent 服务，需要一些监控机制来保证它始终"在线"：

### 定时健康检查

```go
// 伪码：每小时运行一次系统状态检查

const healthCheckPrompt = `
系统自检：
1. 检查磁盘空间是否充足（df -h）
2. 检查 .agent/logs/ 目录大小，超过 100MB 则归档旧日志
3. 检查 .agent/tasks/tasks.json 中是否有长时间卡在 running 状态的任务
4. 报告系统状态
`

// 每小时触发一次健康检查 Task
cronManager.AddJob("0 * * * *", healthCheckPrompt)
```

### 日志管理

```
.agent/logs/
├── mini-agent-20260222-143022.jsonl  # 今天的会话日志
├── mini-agent-20260221-091533.jsonl  # 昨天的
├── mini-agent-20260220-163041.jsonl  # 前天的
└── gateway_20260222_140015.log       # Gateway 日志
```

日志不会自动清理，可以通过 Cron 任务定期归档：

```
你: 设置一个每周日清理 30 天前日志的定时任务

AI: [调用 cron 工具]
    schedule: "0 2 * * 0"（每周日凌晨 2 点）
    description: "清理 .agent/logs/ 中 30 天前的日志文件，使用 bash 工具"
    好的，已设置日志清理定时任务。
```

---

## 记忆的主动触发

Agent 不会自动记录所有信息——它需要判断什么值得记。在 system prompt 中可以引导这种行为：

```
在 system prompt 中加入：

当你发现以下类型的信息时，主动使用 record_note 记录：
- 用户表达的偏好和习惯
- 项目特定的约定和规范
- 常用的配置信息（服务器地址、账号等）
- 你犯过错误后学到的纠正规则

不要记录：
- 临时信息（当前任务的中间状态）
- 可以通过其他方式获取的公开信息
```

有了这样的引导，Agent 会像一个有经验的员工一样，主动积累和维护自己的"工作笔记"。

---

## 记忆管理：增删改查

```
你: 把我的 Server 酱 Key 更新一下，新的是 SCT99999

AI: [调用 recall_note, query: "Server 酱"]
    找到笔记 note-003："Server 酱 Key：SCT12345..."

    [调用 record_note]
    content: "Server 酱 Key（已更新）：SCT99999...（旧Key已作废）"

AI: 已更新记录。原来的记录也保留作为参考，
    你可以发送"删除 note-003"让我清理旧记录。

你: 删除所有和旧项目相关的记忆

AI: [调用 recall_note, query: "旧项目名"]
    找到 5 条相关记录：note-010, note-011, note-012...

    [调用 bash 工具编辑 memory.json 删除这些记录]
    已删除 5 条记录。
```

---

## 模拟对话：记忆驱动的个性化体验

```
[第一次会话]

你: 我不喜欢在代码里写太多注释，保持代码简洁
AI: [调用 record_note: "用户偏好：代码注释最少化，代码本身要有自解释性"]
    记住了！

────────────────────────────────────
[三周后，新会话]

你: 帮我给这段代码写一些注释

AI: [调用 recall_note: "注释"] ← 主动查询记忆
    [读取到偏好：注释最少化]

AI: 根据你的代码风格偏好，我只在确实必要的地方添加注释，
    保持代码简洁：

    func calculateDiscount(price float64, rate float64) float64 {
        // rate 是 0-1 之间的小数，如 0.1 表示 10% 折扣
        return price * (1 - rate)
    }

    （其他方法因逻辑清晰，未添加注释）
```

---

## 本章小结

本章建立了数字员工的"记忆宫殿"：

- **三层记忆**：对话历史（临时）+ 持久笔记（永久）+ Token 摘要（中期）
- **record_note**：将重要信息写入 `.agent/memory.json`，跨会话永久保存
- **recall_note**：关键词检索笔记，按需读取
- **Token 摘要作为工作记忆**：结构化检查点（Goal/Progress/State/Next Steps）+ 增量更新
- **心跳监控**：通过 Cron 任务实现定期健康检查
- **主动记忆触发**：通过 system prompt 引导 Agent 主动积累工作笔记

对应代码：
- `internal/tools/note_tool.go` — 笔记工具实现
- `internal/agent/agent.go:maybeSummarize()` — Token 摘要（第三章已详述）
- `.agent/memory.json` — 持久记忆存储文件

---

## 下一章预告

工具是"动词"（做什么），技能是"方法论"（怎么做）。

技能系统让数字员工不只是会执行指令，更能掌握业务流程和行业知识——代码审查怎么做、文档整理有什么规范、视频处理用哪些步骤……

**[第七章：技能树开花——可插拔的技能系统](ch07-skills.md)**
