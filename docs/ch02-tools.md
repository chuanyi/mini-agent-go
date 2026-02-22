# 第二章：给 AI 装上手脚——工具感知与行动

> 光会说话的 AI 就像一个博学的书呆子——满腹经纶，但打不开冰箱。
> 这章我们给它装上"手脚"。

---

## Function Calling：工具调用原理

LLM 本身只能生成文字。那它是怎么"执行"操作的？

答案是：它不执行，它**告诉你**去执行，然后你把结果告诉它。

```
┌─────────────────────────────────────────────────┐
│                工具调用完整流程                    │
├─────────────────────────────────────────────────┤
│                                                 │
│  1. 你发送消息 + 工具定义（JSON Schema）           │
│     "帮我列出文件" + [ls 工具, read 工具, ...]    │
│                          │                      │
│                          ▼                      │
│  2. LLM 决定用哪个工具                           │
│     "我应该调用 ls 工具，参数是 {path: '.'}"      │
│     （LLM 不会直接执行！它只是输出这个决定）       │
│                          │                      │
│                          ▼                      │
│  3. 你的程序（Agent）执行工具                    │
│     实际运行 ls 命令，得到文件列表                │
│                          │                      │
│                          ▼                      │
│  4. 把结果返回给 LLM                            │
│     "ls 结果：cmd/ internal/ go.mod README.md"  │
│                          │                      │
│                          ▼                      │
│  5. LLM 基于结果生成最终回复                     │
│     "当前目录包含 4 个项目..."                   │
│                                                 │
└─────────────────────────────────────────────────┘
```

**为什么不让 LLM 直接执行？**
- **安全性**：LLM 在云端，不能直接访问你的文件系统
- **可控性**：每次工具调用都经过你的程序，可以做权限检查、日志记录、沙箱隔离
- **可审计性**：所有操作留下记录，知道 AI 干了什么

---

## 工具接口设计

首先定义所有工具必须实现的接口：

```go
// 伪码：工具接口
// 对应真实代码：internal/tools/base.go

// Tool 是所有工具的统一接口
type Tool interface {
    Name() string                                              // 工具名称（LLM 用这个名字调用）
    Description() string                                       // 工具描述（告诉 LLM 这个工具能干什么）
    Parameters() map[string]interface{}                        // 参数定义（JSON Schema 格式）
    Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error)  // 实际执行
    ToSchema() map[string]interface{}      // 转换为 Anthropic 格式
    ToOpenAISchema() map[string]interface{} // 转换为 OpenAI 格式
}

// ToolResult 是工具执行结果
type ToolResult struct {
    Success bool   // 是否成功
    Content string // 给 LLM 看的结果（可能被截断）
    Details string // 给用户界面看的完整结果
    Error   string // 错误信息（失败时）
}
```

**为什么 Content 和 Details 分开？**

`Content` 是发给 LLM 的内容，太长会消耗大量 Token，所以有时候需要截断。
`Details` 是显示在 TUI 或 Web 界面的完整内容，用户看的，不需要省 Token。

---

## BaseTool：避免重复劳动

每个工具都要实现 `ToSchema()` 和 `ToOpenAISchema()`，但这两个方法的逻辑对所有工具都一样，只是格式不同。用嵌入结构体（组合）来复用：

```go
// 伪码：基础工具（提供默认的 Schema 转换实现）
// 对应真实代码：internal/tools/base.go

type BaseTool struct {
    NameValue        string
    DescriptionValue string
    ParametersValue  map[string]interface{}
}

// ToSchema 转换为 Anthropic 格式
func (b *BaseTool) ToSchema() map[string]interface{} {
    return map[string]interface{}{
        "name":         b.NameValue,
        "description":  b.DescriptionValue,
        "input_schema": b.ParametersValue,  // Anthropic 叫 input_schema
    }
}

// ToOpenAISchema 转换为 OpenAI 格式
func (b *BaseTool) ToOpenAISchema() map[string]interface{} {
    return map[string]interface{}{
        "type": "function",
        "function": map[string]interface{}{
            "name":        b.NameValue,
            "description": b.DescriptionValue,
            "parameters":  b.ParametersValue,  // OpenAI 叫 parameters
        },
    }
}
```

具体工具只需嵌入 `BaseTool`，专注实现 `Execute()` 方法即可。

---

## 实现一个真实工具：ls

来看一个完整的工具实现——列目录（`ls`）：

```go
// 伪码：ls 工具实现
// 对应真实代码：internal/tools/ls_tool.go

type LSTool struct {
    BaseTool           // 嵌入，获得 ToSchema/ToOpenAISchema
    workspace string   // 工作目录（用于路径安全检查）
}

func NewLSTool(workspace string) *LSTool {
    return &LSTool{
        BaseTool: BaseTool{
            NameValue:        "ls",
            DescriptionValue: "列出目录内容，支持递归和隐藏文件",
            ParametersValue: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "path": map[string]interface{}{
                        "type":        "string",
                        "description": "目录路径（相对于工作目录或绝对路径）",
                    },
                },
                "required": []string{},  // path 可选，默认工作目录
            },
        },
        workspace: workspace,
    }
}

func (t *LSTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
    // 获取参数（路径可选，默认当前目录）
    path := OptionalString(params, "path", ".")

    // 安全检查：防止目录遍历攻击（../../etc/passwd 这类）
    resolved, err := ResolvePath(t.workspace, path)
    if err != nil {
        return ErrorResult(err.Error()), nil
    }

    // 实际列目录
    entries, err := os.ReadDir(resolved)
    if err != nil {
        return ErrorResult(fmt.Sprintf("无法读取目录: %v", err)), nil
    }

    // 格式化输出
    var lines []string
    for _, entry := range entries {
        if entry.IsDir() {
            lines = append(lines, entry.Name()+"/")
        } else {
            lines = append(lines, entry.Name())
        }
    }

    output := strings.Join(lines, "\n")
    return ContentResult(output), nil
}
```

---

## 路径安全：防止目录遍历

这是工具系统里最重要的安全机制：

```go
// 伪码：路径安全检查
// 对应真实代码：internal/tools/base.go:ResolvePath

func ResolvePath(workspace, filePath string) (string, error) {
    // 相对路径基于工作目录解析
    if !filepath.IsAbs(filePath) {
        filePath = filepath.Join(workspace, filePath)
    }

    // 清理路径（解析 .. 等）
    resolved := filepath.Clean(filepath.Abs(filePath))
    wsAbs := filepath.Abs(workspace)

    // 检查：结果必须在工作目录内
    if !strings.HasPrefix(resolved, wsAbs) {
        return "", fmt.Errorf("访问拒绝：路径 %q 超出工作目录 %q", filePath, workspace)
    }

    return resolved, nil
}
```

这样，即使 LLM 被"注入攻击"要求访问 `../../etc/passwd`，也会被这里拦住。

---

## Bash 工具：最强大的工具

文件工具覆盖了常见场景，但有时你需要运行任意命令——安装依赖、执行脚本、调用 CLI 工具。这就是 Bash 工具：

```go
// 伪码：Bash 工具核心逻辑
// 对应真实代码：internal/tools/bash_tool.go

type BashTool struct {
    BaseTool
    workspace string
    shell     *shell.Shell  // 持久化 Shell 会话
}

func (t *BashTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
    command := params["command"].(string)
    timeout := params["timeout"].(int)  // 秒，默认 30

    // 在持久化 Shell 中执行命令
    output, err := t.shell.Execute(ctx, command, timeout)

    // 截断过长的输出（太长会耗尽 Token）
    if len(output) > maxOutputBytes {
        output = output[:maxOutputBytes] + "\n... [输出已截断]"
    }

    return ContentResult(output), nil
}
```

> 💡 **Claude Code 实现决策**：使用 `mvdan.cc/sh/v3`——一个纯 Go 实现的 POSIX shell 解释器。好处是：
> 1. 跨平台（Windows 上也能跑 `ls`、`grep` 等命令）
> 2. 无需系统安装 bash（某些 Windows 环境没有）
> 3. 安全性更好（可以限制能执行的操作）

**持久化 Shell 会话**的意义：

```
# 普通工具调用（无状态）：
调用1: cd /tmp    →  OK（但这个 cd 不影响下一次）
调用2: ls         →  仍然列出原来目录

# 持久化 Shell（有状态）：
调用1: cd /tmp    →  OK（Shell 记住了当前目录）
调用2: ls         →  列出 /tmp 的内容 ✓
调用3: export FOO=bar  →  OK
调用4: echo $FOO  →  输出 "bar" ✓
```

这对于需要多步操作的任务非常重要：`cd` 到某个目录，然后再执行命令。

---

## 工具注册表：工厂管理

所有工具统一在注册表中管理：

```go
// 伪码：工具注册表
// 对应真实代码：internal/tools/base.go

type ToolRegistry struct {
    tools map[string]Tool
}

// 注册工具
func (r *ToolRegistry) Register(tool Tool) {
    r.tools[tool.Name()] = tool
}

// 按名称获取（Agent 执行工具时用）
func (r *ToolRegistry) Get(name string) (Tool, bool) {
    tool, ok := r.tools[name]
    return tool, ok
}

// 获取所有工具的 Schema（发给 LLM 时用）
func (r *ToolRegistry) GetSchemas() []map[string]interface{} {
    var schemas []map[string]interface{}
    for _, tool := range r.tools {
        schemas = append(schemas, tool.ToSchema())
    }
    return schemas
}
```

---

## 默认工具集一览

项目内置了这些工具：

| 工具名 | 功能 |
|--------|------|
| `ls` | 列目录 |
| `read` | 读文件 |
| `write` | 写文件 |
| `edit` | 精确替换文件中的文本 |
| `multiedit` | 批量编辑多处文本 |
| `grep` | 内容搜索（使用 ripgrep） |
| `find` | 文件名搜索（使用 fd） |
| `bash` | 执行 Shell 命令 |
| `record_note` | 写入持久记忆 |
| `recall_note` | 读取持久记忆 |
| `web_fetch` | 抓取网页内容 |
| `web_download` | 下载文件 |
| `todo` | 待办事项管理 |

**注册代码**：

```go
// 伪码：默认工具注册
// 对应真实代码：internal/agent/agent.go:RegisterDefaultTools

func (a *Agent) RegisterDefaultTools() {
    // 文件工具
    a.RegisterTool(tools.NewLSTool(a.workspace))
    a.RegisterTool(tools.NewReadTool(a.workspace))
    a.RegisterTool(tools.NewWriteTool(a.workspace))
    a.RegisterTool(tools.NewEditTool(a.workspace))
    a.RegisterTool(tools.NewMultiEditTool(a.workspace))
    a.RegisterTool(tools.NewGrepTool(a.workspace))
    a.RegisterTool(tools.NewFindTool(a.workspace))

    // 执行工具
    a.RegisterTool(tools.NewBashTool(a.workspace))

    // 记忆工具（持久化到 .agent/memory.json）
    memoryFile := filepath.Join(agentDir, "memory.json")
    a.RegisterTool(tools.NewRecordNoteTool(memoryFile))
    a.RegisterTool(tools.NewRecallNoteTool(memoryFile))

    // 网络工具
    a.RegisterTool(tools.NewWebFetchTool())
    a.RegisterTool(tools.NewWebDownloadTool(a.workspace))

    // 待办工具
    a.RegisterTool(tools.NewTodoTool(a.todoStorage, a.eventCh))
}
```

---

## 模拟对话：工具调用实战

```
你: 帮我看看 go.mod 文件，然后告诉我项目依赖了哪些外部包

AI: 好的，让我先读取 go.mod 文件。
    [调用 read 工具，参数：{"file_path": "go.mod"}]

[工具执行结果]
    module mini-agent-go
    go 1.21
    require (
        github.com/anthropics/anthropic-sdk-go v0.2.0
        mvdan.cc/sh/v3 v3.8.0
        github.com/aymanbagabas/go-udiff v0.2.0
        ...
    )

AI: 从 go.mod 来看，项目依赖了以下外部包：

    核心依赖：
    • anthropic-sdk-go — Anthropic 官方 Go SDK，用于调用 Claude API
    • mvdan.cc/sh/v3 — 纯 Go 实现的 POSIX Shell，用于 bash 工具

    工具类：
    • go-udiff — 统一差异格式（unified diff），用于 edit 工具
    • html-to-markdown — 网页转 Markdown，用于 web_fetch 工具
    • goquery — HTML 解析

    所有依赖都很轻量，没有重型框架。
```

---

## 扩展方向：浏览器自动化

工具系统是可扩展的。一个很有意思的扩展方向是**浏览器自动化**：

```
[浏览器工具调用流程]

AI 调用 browser_screenshot 工具
         │
         ▼
工具启动无头浏览器（如 chromedp），截取当前页面截图
         │
         ▼
截图以 base64 格式返回给 LLM（视觉模型）
         │
         ▼
LLM 分析截图，决定下一步操作：
"我看到登录按钮在右上角，需要点击它"
         │
         ▼
AI 调用 browser_click 工具，传入坐标
         │
         ▼
循环，直到任务完成
```

这就是 AI 自动填表、自动购物、自动抢票的原理。本项目的工具接口完全支持这种扩展——只需实现 `Tool` 接口就行。

---

## 本章小结

本章构建了 AI 的"手脚"：

- **Function Calling 原理**：LLM 不执行，只决策；Agent 执行，再汇报
- **Tool 接口**：`Name / Description / Parameters / Execute / ToSchema`
- **BaseTool**：嵌入复用 Schema 转换逻辑
- **路径安全**：`ResolvePath` 防止目录遍历攻击
- **Bash 工具**：持久化 Shell 会话，支持有状态操作
- **ToolRegistry**：工厂模式统一管理工具注册与查询

对应代码：
- `internal/tools/base.go` — 接口定义、注册表、路径安全、参数提取
- `internal/tools/ls_tool.go` — ls 工具（典型实现参考）
- `internal/tools/bash_tool.go` — bash 工具（持久化 Shell）
- `internal/tools/note_tool.go` — 记忆工具
- `internal/tools/web*.go` — 网络工具

---

## 下一章预告

工具有了，单次对话有了，但这还只是"聊天"。真正的 Agent 需要**持续思考**、**自主循环**、还要**聪明地管理自己的记忆空间**——不然"记"太多就会崩溃。

**[第三章：让 AI 自主奔跑——Agent 循环与智能摘要](ch03-agent-loop.md)**
