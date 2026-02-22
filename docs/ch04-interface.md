# 第四章：全渠道驾驶舱——TUI/Web/通知/IM

> Agent 跑起来了，但你不可能一直盯着终端。
> 这章我们给它装上"驾驶舱"——从终端 UI 到手机 IM，随时随地掌控你的数字员工。

---

## 为什么需要多种界面？

单一终端输入有很多局限：
- 你在外面，Agent 在服务器跑任务，你看不到进度
- 你希望通过手机微信给 Agent 下指令
- 你希望任务完成时主动推送到手机，不用一直等着

一个成熟的数字员工需要**全渠道接入**——终端、浏览器、手机，任选其一。

---

## TUI：终端用户界面

### Bubbletea 框架

TUI 基于 [Bubbletea](https://github.com/charmbracelet/bubbletea) 框架，它采用 **Elm 架构**：

```
┌─────────────────────────────────────────────────────┐
│                   Elm 架构                          │
│                                                     │
│   Model (状态)  ──► View (渲染)                     │
│       ▲                                             │
│       │                                             │
│   Update (状态更新) ◄── Msg (事件/消息)              │
│                                                     │
│  所有状态变更都经过 Update，UI 只是状态的映射         │
└─────────────────────────────────────────────────────┘
```

```go
// 伪码：TUI 核心结构
// 对应真实代码：internal/tui/

type Model struct {
    // 对话内容
    messages []DisplayMessage
    input    string           // 当前输入框内容

    // Agent 状态
    agentRunning bool
    currentTool  string       // 正在执行的工具名

    // 界面状态
    viewport viewport.Model  // 可滚动的消息区域
    inputBox textinput.Model
}

// Update 处理各种事件
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.Type == tea.KeyEnter {
            // 用户按回车，发送消息
            return m.sendMessage()
        }

    case AgentStreamDelta:
        // AI 正在输出文字（流式）
        m.appendToLastMessage(msg.Delta)

    case AgentToolCall:
        // AI 调用了工具
        m.addToolCallEntry(msg.ToolName, msg.Args)

    case AgentToolResult:
        // 工具执行完成
        m.updateToolCallResult(msg.Success, msg.Details)

    case AgentComplete:
        // Agent 完成任务
        m.agentRunning = false
    }
    return m, nil
}
```

### 工具调用折叠展示

TUI 的一个重要设计：工具调用默认折叠，点击展开：

```
╭─────────────────────────────────────────────────────╮
│ 好的，让我先看看项目结构。                            │
│                                                     │
│ ▶ [ls] . ✓                   （折叠状态，显示√成功）  │
│ ▶ [read] go.mod ✓                                   │
│ ▶ [write] report.md ✓                               │
│                                                     │
│ 分析完成！项目包含 6 个核心模块...                   │
╰─────────────────────────────────────────────────────╯
> 输入你的指令...
```

展开后：

```
╭─────────────────────────────────────────────────────╮
│ ▼ [ls] .                     （展开状态）            │
│   cmd/                                              │
│   internal/                                         │
│   go.mod                                            │
│   README.md                                         │
╰─────────────────────────────────────────────────────╯
```

这样既能一眼看到执行摘要，又能在需要时查看详情。

### AgentCallback：连接 Agent 和 TUI

```go
// 伪码：回调接口
// 对应真实代码：internal/agent/callback.go

type AgentCallback struct {
    OnStep           func(step, maxSteps int)           // 步骤更新
    OnStreamComplete func(content string)               // 流式输出完成
    OnToolCall       func(name, args string)            // 工具调用开始
    OnToolResult     func(name string, success bool, details string) // 工具结果
}
```

TUI 实现这些回调，将 Agent 的执行状态实时映射到界面更新。

---

## 远程 Web 控制

### 为什么需要远程控制？

最常见的使用场景：
- Agent 在家里的服务器上跑长时间任务
- 你在外面，想查看进度或追加指令
- 不想在手机上装 SSH 客户端

### WebSocket 实时推送

```
浏览器（手机/电脑）          Agent 服务器
      │                           │
      │ GET /qr                   │
      │ ─────────────────────────►│
      │ 返回二维码                │
      │ ◄─────────────────────────│
      │                           │
      │ [扫码] WS /chat           │
      │ ─────────────────────────►│  建立 WebSocket 连接
      │                           │
      │                           │  Agent 执行中...
      │ ◄── stream delta ─────────│  实时推送每个文字 token
      │ ◄── tool_call ────────────│  推送工具调用事件
      │ ◄── tool_result ──────────│  推送工具结果
      │ ◄── complete ─────────────│  任务完成
```

```go
// 伪码：WebSocket 消息协议
type WSMessage struct {
    Type    string      `json:"type"`    // "delta" | "tool_call" | "complete"
    Content string      `json:"content"` // 文字内容
    Tool    string      `json:"tool"`    // 工具名
    Success bool        `json:"success"` // 工具是否成功
}
```

### QR 码快速连接

```go
// 伪码：启动时生成 QR 码
// 对应真实代码：cmd/main.go 启动逻辑

localIP := getLocalIP()       // 获取本机局域网 IP
url := fmt.Sprintf("http://%s:%d", localIP, port)
qrCode := generateQR(url)    // 生成终端可显示的 QR 码
fmt.Println(qrCode)          // 打印到终端
fmt.Printf("扫码访问: %s\n", url)
```

启动后终端会显示一个 ASCII 二维码，手机扫描即可打开 Web 界面。

---

## 通知系统（Server 酱）

任务跑在后台，完成时怎么通知你？

```go
// 伪码：Server 酱推送
// 对应真实代码：内置通知工具

func NotifyServerChan(title, content string) error {
    webhookURL := config.Notify.ServerChanKey

    // Server 酱 Webhook：HTTP POST 即可推送到微信
    resp, err := http.PostForm(webhookURL, url.Values{
        "title":   {title},
        "desp":    {content},
    })
    // ...
}
```

配置：

```yaml
notify:
  server_chan_key: "SCT..."   # Server 酱的 Webhook Key
```

当 Task 完成时（第五章会详讲），Agent 自动调用推送，你的手机微信就收到一条消息：

```
[数字员工通知]
任务完成：代码审查报告
项目 internal/tools/ 代码审查完成，
共发现 3 处安全隐患，2 处性能优化点。
报告已保存至 review-2026-02-22.md
```

---

## IM Gateway：让 Agent 成为 IM 机器人

这是最有趣的功能——让你的数字员工通过微信、企业微信等 IM 工具与你对话。

### 架构设计

```
手机 IM                  Gateway                    Agent Worker
   │                        │                           │
   │── "帮我查天气" ────────►│                           │
   │                        │── 路由到对应 Worker ──────►│
   │                        │                           │ 思考中...
   │                        │                           │ 调用工具...
   │                        │◄── "北京今天晴，22℃" ──────│
   │◄── "北京今天晴，22℃" ──│                           │
```

```go
// 伪码：Gateway 核心结构
// 对应真实代码：internal/gateway/gateway.go

type Gateway struct {
    inbound  chan *GatewayMessage  // 所有 IM 渠道的入站消息
    outbound chan *GatewayMessage  // 待发送的回复消息
    channels map[string]IMChannel  // 注册的 IM 渠道
    workers  map[string]*agentWorker  // 每个渠道对应一个 Agent 实例
}

// agentWorker 封装了一个 Agent 实例和它的消息队列
type agentWorker struct {
    msgCh chan *GatewayMessage  // 入站消息队列
    outCh chan<- *GatewayMessage // 出站回复通道
    ag    *agent.Agent          // 专属 Agent 实例（有独立对话历史）
}
```

**关键设计：每个渠道账号对应一个独立的 Agent 实例**。这样不同用户的对话历史完全隔离，互不干扰。

### IMChannel 接口

任何 IM 渠道都实现同一个接口：

```go
// 伪码：IM 渠道接口
// 对应真实代码：internal/gateway/channel.go

type IMChannel interface {
    Type() string                                          // "server_im" | "wechat" | ...
    AccountID() string                                     // 账号标识符
    Start(ctx context.Context, inbound chan<- *GatewayMessage) error  // 开始监听消息
    Send(ctx context.Context, msg *GatewayMessage) error  // 发送回复
    Stop() error                                          // 停止
}
```

### Server 酱 IM 渠道实现

项目内置了 Server 酱 IM 渠道（支持微信接收消息）：

```go
// 伪码：Server 酱 IM 渠道
// 对应真实代码：internal/gateway/channels/server_im.go

type ServerIMChannel struct {
    accountID  string
    webhookKey string  // 用于发送消息
    pollURL    string  // 用于轮询接收消息
    logger     *log.Logger
}

// Start 轮询模式：定期请求 Server 酱 API 检查新消息
func (c *ServerIMChannel) Start(ctx context.Context, inbound chan<- *GatewayMessage) error {
    ticker := time.NewTicker(5 * time.Second)  // 每 5 秒检查一次
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            // 拉取新消息
            messages, err := c.pollMessages()
            for _, msg := range messages {
                inbound <- &GatewayMessage{
                    ChannelType: c.Type(),
                    AccountID:   c.AccountID(),
                    Content:     msg.Content,
                }
            }
        }
    }
}

// Send 发送回复
func (c *ServerIMChannel) Send(ctx context.Context, msg *GatewayMessage) error {
    return postToServerChan(c.webhookKey, msg.Content)
}
```

### 消息路由

```go
// 伪码：消息路由循环
// 对应真实代码：internal/gateway/gateway.go:dispatchLoop()

func (g *Gateway) dispatchLoop() {
    for msg := range g.inbound {
        // 根据渠道类型+账号 ID 找到对应的 Worker
        key := msg.ChannelType + ":" + msg.AccountID
        worker := g.workers[key]

        // 投递给 Worker（异步，不等 Agent 响应）
        worker.msgCh <- msg
    }
}

func (g *Gateway) sendLoop() {
    for msg := range g.outbound {
        // 找到对应渠道发送回复
        ch := g.channels[msg.ChannelType+":"+msg.AccountID]
        ch.Send(g.ctx, msg)
    }
}
```

### 添加自己的 IM 渠道

实现 `IMChannel` 接口，注册到 Gateway：

```go
// 伪码：自定义渠道注册

myChannel := &MyCustomIMChannel{
    accountID: "my-account",
    // ...配置...
}

gateway.AddChannel(myChannel)
gateway.Start()
```

Webhook 模式（消息推过来，不需要轮询）：

```go
// 伪码：Webhook 渠道（等 IM 平台推消息过来）
func (c *WebhookChannel) Start(ctx context.Context, inbound chan<- *GatewayMessage) error {
    http.HandleFunc("/webhook/im", func(w http.ResponseWriter, r *http.Request) {
        msg := parseWebhookPayload(r.Body)
        inbound <- &GatewayMessage{Content: msg.Text}
        w.WriteHeader(200)
    })
    return http.ListenAndServe(":8080", nil)
}
```

---

## 模拟对话：手机 IM 控制 Agent

```
[手机微信 - 数字员工 Bot]

你: 帮我看一下项目里有没有 TODO 注释，整理成列表
数字员工: 好的，正在搜索项目中的 TODO 注释...
         （实际上 Agent 在服务器运行了 grep 工具）

[约 8 秒后]
数字员工: 找到以下 TODO：

          internal/tools/bash_tool.go:45
          // TODO: 添加命令黑名单过滤

          internal/llm/openai_sdk.go:128
          // TODO: 支持多轮工具调用并行执行

          cmd/main.go:89
          // TODO: 添加配置热重载支持

          共 3 处，已整理完毕。

你: /clear
数字员工: 会话已重置
```

`/clear` 命令清空对话历史，这是内置的特殊命令，让 IM 模式的 Agent 可以重新开始。

---

## 本章小结

本章给 Agent 装上了全渠道接入能力：

- **TUI**：Bubbletea Elm 架构，工具调用折叠展示，流式输出
- **AgentCallback**：连接 Agent 和 UI 的回调接口
- **远程 Web 控制**：WebSocket 实时推送 + QR 码扫码连接
- **通知系统**：Server 酱 Webhook，任务完成主动推送
- **IM Gateway**：每渠道独立 Agent、消息路由、Server 酱 IM 渠道实现
- **IMChannel 接口**：可扩展，添加任意 IM 渠道

对应代码：
- `internal/tui/` — TUI 实现
- `internal/gateway/gateway.go` — 消息路由核心
- `internal/gateway/channel.go` — IMChannel 接口
- `internal/gateway/channels/` — 具体渠道实现

---

## 下一章预告

数字员工已经很能干了，但它一次只能做一件事。如果你有 100 个文件要处理，或者希望每天定时自动生成报告，该怎么办？

**[第五章：分身有术——子 Agent 与异步任务](ch05-async.md)**
