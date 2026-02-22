# 第一章：与 AI 握手——从一行 API 调用开始

> 想象你雇了一位无所不知的顾问，随叫随到，从不抱怨，按调用次数收费。
> 一切都从一次简单的 HTTP 请求开始。

---

## 为什么是 Go 语言？

在开始之前，先回答一个问题：市面上有那么多 AI Agent 框架，Python 的、JavaScript 的，为什么要用 Go？

> 💡 **Claude Code 实现决策**：在项目初期，Claude Code 建议使用 Go，理由是：
> 1. **并发原生支持**：Agent 需要同时处理流式输出、工具执行、定时任务——Go 的 goroutine 让这些天然简单
> 2. **单二进制部署**：`go build` 就是一个可执行文件，扔到服务器上就能跑，不需要 Python 虚拟环境
> 3. **资源消耗低**：Agent 常驻后台运行，Go 的内存占用比 Python 低一个数量级
> 4. **类型安全**：与 LLM 交互时有大量 JSON 结构，强类型能在编译期捕获很多错误

简单说：Go 非常适合"跑在服务器后台、长期稳定工作"这个场景，正好是数字员工的典型用法。

---

## LLM API 的本质

很多人觉得调用 AI 很神秘。其实，Anthropic、OpenAI 的 API 没有任何魔法——它就是一个 HTTP POST 请求，发送 JSON，收到 JSON。

```
你的程序                    Anthropic 服务器
    │                              │
    │── POST /v1/messages ────────►│
    │   {                          │
    │     "model": "claude-...",   │
    │     "messages": [...]        │  ◄── LLM 在这里推理
    │   }                          │
    │                              │
    │◄──────── 200 OK ─────────────│
    │   {                          │
    │     "content": [             │
    │       {"text": "你好！..."}  │
    │     ]                        │
    │   }                          │
```

本质上，你在发邮件，对方（超级智能）回复你。

---

## 统一数据结构：为什么要抽象？

如果直接用 Anthropic 的数据格式，下次想换成 OpenAI 或者 DeepSeek，就要改很多地方。所以我们先定义一套**内部统一格式**，再分别转换成各家 API 的格式。

这就是 `internal/schema/schema.go` 做的事：

```go
// 伪码：内部统一消息格式
// 对应真实代码：internal/schema/schema.go

// Message 是对话中的一条消息
type Message struct {
    Role      string      // "user" | "assistant" | "system" | "tool"
    Content   string      // 文本内容
    ToolCalls []ToolCall  // AI 决定调用的工具列表
    ToolCallID string     // 工具结果对应的调用 ID
}

// ToolCall 是 AI 决定调用的一个工具
type ToolCall struct {
    ID       string                 // 唯一标识符（匹配工具结果用）
    Function struct {
        Name      string                 // 工具名称
        Arguments map[string]interface{} // 参数（JSON 解析后）
    }
}

// LLMResponse 是 LLM 返回的完整响应
type LLMResponse struct {
    Content      string      // 文字回复
    ToolCalls    []ToolCall  // 工具调用请求（如果有）
    FinishReason string      // "end_turn" | "tool_use" | "max_tokens"
    Usage        *Usage      // Token 使用统计
}

// Usage 用于 Token 计费和监控
type Usage struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
}
```

**Provider 常量**：

```go
type LLMProvider string

const (
    ProviderAnthropic LLMProvider = "anthropic"
    ProviderOpenAI    LLMProvider = "openai"
)
```

为什么 `Arguments` 用 `map[string]interface{}`？因为不同工具的参数结构完全不同，用 `map` 最灵活，在执行时再按工具定义去解析。

---

## Client 接口：LLM 的统一"插座"

有了数据结构，接下来定义操作接口：

```go
// 伪码：LLM 客户端接口
// 对应真实代码：internal/llm/client.go

// Client 是所有 LLM 客户端必须实现的接口
type Client interface {
    // GenerateStream 发送消息并接收流式响应
    // callback 为 nil 时退化为普通（非流式）调用
    GenerateStream(
        ctx      context.Context,
        messages []*Message,         // 对话历史
        tools    []map[string]any,   // 工具定义（JSON Schema 格式）
        callback StreamCallback,     // 流式回调（可为 nil）
    ) (*LLMResponse, error)
}

// StreamCallback 接收流式事件
type StreamCallback interface {
    OnContentDelta(delta string) error          // 文字片段到达
    OnThinkingDelta(delta string) error         // 思考过程片段（Claude 3.7+）
    OnToolCallStart(id, name string) error      // 工具调用开始
    OnToolCallDelta(id, partialJSON string) error // 工具参数流式传输
    OnToolCallComplete(id string, input map[string]any) error // 工具调用完整参数
    OnUsageUpdate(usage *Usage) error           // Token 使用量更新
}
```

**为什么要流式？**

非流式调用意味着你等 LLM 把完整回复生成完才能看到任何内容——对于长回复可能要等几十秒。流式输出让文字一个字一个字地出现，就像看到 AI 在"打字"，体验好很多。

> 💡 **Claude Code 实现决策**：`StreamCallback` 接口而不是 `chan string`。使用接口的好处是：不同的消费者（TUI、Web、IM Bot）可以实现不同的回调逻辑，而不需要在通道两端加复杂的调度逻辑。当 `callback` 为 `nil` 时，`GenerateStream` 自动退化为普通调用，两种模式共用一套实现。

---

## Anthropic SDK 实现

接下来是真正和 Anthropic 服务器通话的代码：

```go
// 伪码：Anthropic 客户端实现
// 对应真实代码：internal/llm/anthropic_sdk.go

type AnthropicClient struct {
    client *anthropic.Client  // 官方 SDK 客户端
    model  string
    config Config
}

func (c *AnthropicClient) GenerateStream(ctx, messages, tools, callback) (*LLMResponse, error) {
    // 第一步：把内部格式转换为 Anthropic 格式
    anthropicMessages := convertMessages(messages)
    anthropicTools := convertTools(tools)

    // 第二步：发起流式请求（带重试）
    for attempt := 0; attempt < maxRetries; attempt++ {
        stream, err := c.client.Messages.NewStreaming(ctx, params)
        if err != nil {
            if isRetryable(err) {
                time.Sleep(backoff(attempt))
                continue
            }
            return nil, err
        }

        // 第三步：处理流式事件
        for event := range stream {
            switch event.Type {
            case "content_block_delta":
                if callback != nil {
                    callback.OnContentDelta(event.Delta.Text)
                }
            case "tool_use":
                if callback != nil {
                    callback.OnToolCallStart(event.ID, event.Name)
                }
            }
        }

        return buildResponse(stream.FinalMessage()), nil
    }
}
```

**消息格式转换的关键点**：

Anthropic 的格式和我们内部格式有几处不同：

1. **工具结果**：Anthropic 把工具结果放在 `user` 角色的消息里（特殊的 `tool_result` 内容块），而我们内部用独立的 `tool` 角色
2. **多模态内容**：Anthropic 的 `content` 可以是字符串数组，内部统一用单字符串
3. **工具调用结果匹配**：通过 `tool_use_id` 把工具结果与工具调用对应起来

---

## OpenAI SDK 实现

OpenAI 格式和 Anthropic 格式的主要区别：

```
                  Anthropic                  OpenAI
工具结果角色:     "user" (tool_result)       "tool"
工具定义格式:     {input_schema: {...}}      {parameters: {...}}
流式工具参数:     完整 JSON                  分片 JSON 字符串
```

```go
// 伪码：OpenAI 客户端（简化）
// 对应真实代码：internal/llm/openai_sdk.go

func convertToOpenAIMessages(messages []*Message) []openai.ChatCompletionMessage {
    var result []openai.ChatCompletionMessage
    for _, msg := range messages {
        switch msg.Role {
        case "tool":
            // OpenAI 工具结果是独立角色
            result = append(result, openai.ChatCompletionMessage{
                Role:       "tool",
                Content:    msg.Content,
                ToolCallID: msg.ToolCallID,
            })
        // ... 其他角色处理
        }
    }
    return result
}
```

---

## 创建客户端：工厂函数

```go
// 伪码：工厂函数
// 对应真实代码：internal/llm/client.go

func NewClient(config Config) (Client, error) {
    switch config.Provider {
    case ProviderAnthropic:
        return NewAnthropicSDKClient(config)
    case ProviderOpenAI:
        return NewOpenAISDKClient(config)
    default:
        return nil, fmt.Errorf("不支持的 Provider: %s", config.Provider)
    }
}
```

这就是工厂模式的价值——调用者不需要知道底层用了哪个 SDK，只管用 `Client` 接口。

---

## 第一次对话：运行效果

把上面所有代码组合起来，就可以和 AI 说话了：

```go
// 伪码：最简对话
client, _ := llm.NewClient(llm.Config{
    Provider: schema.ProviderAnthropic,
    Model:    "claude-sonnet-4-6",
    APIKey:   "sk-ant-...",
})

messages := []*schema.Message{
    {Role: "user", Content: "你好，请介绍一下你自己"},
}

response, _ := client.GenerateStream(ctx, messages, nil, nil)
fmt.Println(response.Content)
```

```
模拟运行效果：

你: 你好，请介绍一下你自己
AI: 你好！我是 Claude，Anthropic 开发的 AI 助手。
    我可以帮你分析信息、编写代码、解答问题、
    甚至完成各种复杂的任务……

    （token 使用：输入 28 tokens，输出 52 tokens）
```

---

## 配置加载

实际使用时，API Key 不应该硬编码。项目通过 YAML 配置文件管理：

```go
// 伪码：配置结构
// 对应真实代码：internal/config/config.go

type Config struct {
    LLM struct {
        Provider    string  // "anthropic" 或 "openai"
        Model       string  // 模型名称
        APIKey      string  // API Key
        APIBase     string  // 自定义 API Base（OpenAI 兼容接口用）
        MaxRetries  int     // 失败重试次数
        MaxTokens   int     // 最大输出 tokens
    }
    Agent struct {
        MaxSteps       int    // 最大步骤数（防无限循环）
        TokenLimit     int    // 触发摘要的 Token 阈值
        WorkspaceDir   string // 工作目录
        SystemPromptPath string // 自定义系统提示文件路径
    }
}
```

配置文件查找顺序（优先级从高到低）：
1. 可执行文件同级的 `.agent/config/config.yaml`
2. 用户主目录 `~/.mini-agent/config/config.yaml`
3. 当前目录 `config/config.yaml`

---

## 本章小结

本章建立了整个框架的基础：

- **为什么用 Go**：并发、轻量、单二进制、类型安全
- **LLM API 的本质**：HTTP POST + JSON，没有魔法
- **统一 Schema**：`Message`、`ToolCall`、`LLMResponse`，屏蔽 Provider 差异
- **Client 接口**：`GenerateStream` 方法 + `StreamCallback` 接口，流式和非流式统一
- **两个实现**：`AnthropicSDKClient` 和 `OpenAISDKClient`，工厂函数创建

对应代码：
- `internal/schema/schema.go` — 数据结构定义
- `internal/llm/client.go` — 接口定义与工厂函数
- `internal/llm/anthropic_sdk.go` — Anthropic 实现
- `internal/llm/openai_sdk.go` — OpenAI 兼容实现
- `internal/config/config.go` — 配置加载

---

## 下一章预告

光会说话的 AI 有什么用？真正的助手需要能做事——读文件、写代码、搜索网页……

**[第二章：给 AI 装上手脚——工具感知与行动](ch02-tools.md)**

我们将实现 Function Calling，让 AI 不只是"嘴炮"，而是真正能干活的数字员工。
