# 《用 Claude Code 打造你的智能体数字员工》教程总纲

---

## 这套教程是怎么来的？

2026马年春节几天实在无聊，通过和Claude Code来回协作， 从零构建了一个完整的 Go 语言 AI Agent 框架mini-agent-go，完成整个设计、实现、调试、重构。最终的成果是一个真正可以投入使用的"数字员工"系统：它能调用工具、自主完成任务、记忆你的偏好、定时汇报工作，甚至通过即时通讯跟你聊天。

这套教程，就是把那段构建历程变成一个循序渐进的学习路径。

**你将学到的不只是代码，还有一种新的工作方式**：与 AI 协作，让它成为你最得力的数字员工。

---

## 这套教程适合谁？

- 了解 Go 语言基础（不需要是专家）
- 对 AI、LLM、Agent 感兴趣但不知从哪下手
- 想把 AI 用到实际工作中，而不仅仅是聊天
- 喜欢"看着东西跑起来"而不是读纯理论

**不需要**：深度学习背景、LLM 论文阅读经历、任何 AI 框架使用经验。

---

## 最终你能构建什么？

```
✅ 能理解自然语言、完成复杂任务的 AI Agent
✅ 支持文件读写、命令执行、网页搜索等工具调用
✅ 自动管理上下文，不会因"记忆太多"而崩溃
✅ 终端 TUI + 远程 Web 控制双界面
✅ 微信/IM 机器人集成
✅ 定时任务与异步子 Agent
✅ 持久记忆与技能插件系统
```

---

## 章节地图

```
第1章 ──────► 第2章 ──────► 第3章
LLM API       工具系统       Agent循环
（一次对话）   （有手有脚）    （自主奔跑）
                              │
                              ▼
              第6章 ◄──── 第4章 ──────► 第5章
              记忆系统      多界面         子Agent
              （记住你）    （驾驶舱）      （分身）
                              │
                              ▼
              第7章 ──────► 第8章 ──────► 第9章
              技能系统       自我进化       无限可能
              （技能树）     （换大脑）      （你的想象）
```

**可跳读指南**：
- 只想了解原理 → 第1、2、3章
- 想搭建实用工具 → 加上第4、5章
- 想打造专属助手 → 全部章节
- 对某个话题感兴趣 → 直接跳到对应章节

---

## 章节目录

| 章节 | 标题 | 核心主题 |
|------|------|----------|
| [第一章](ch01-llm-api.md) | 与 AI 握手——从一行 API 调用开始 | LLM 接口、统一抽象、流式输出 |
| [第二章](ch02-tools.md) | 给 AI 装上手脚——工具感知与行动 | Function Calling、工具接口、文件/Bash 工具 |
| [第三章](ch03-agent-loop.md) | 让 AI 自主奔跑——Agent 循环与智能摘要 | 主循环、Token 管理、自动压缩 |
| [第四章](ch04-interface.md) | 全渠道驾驶舱——TUI/Web/通知/IM | 多界面、WebSocket、IM Gateway |
| [第五章](ch05-async.md) | 分身有术——子 Agent 与异步任务 | TaskManager、Cron、并发控制 |
| [第六章](ch06-memory.md) | 记忆宫殿——持久内存与心跳监控 | 笔记工具、Token 摘要、监控 |
| [第七章](ch07-skills.md) | 技能树开花——可插拔的技能系统 | Skills、渐进式披露、SKILL.md |
| [第八章](ch08-evolution.md) | 自我进化——模型切换与能力扩展 | LLM 切换、外部工具、MCP |
| [第九章](ch09-imagination.md) | 无限可能——应用场景与你的想象 | 应用展望、创意激发 |

---

## 环境准备

### 1. 安装 Go

访问 [go.dev/dl](https://go.dev/dl/) 下载 Go 1.21+。

```bash
go version  # 确认安装成功
# go version go1.21.0 ...
```

### 2. 获取 API Key

本项目支持两种 LLM 提供商：

**Anthropic Claude**（推荐）：
- 访问 [console.anthropic.com](https://console.anthropic.com)
- 创建账号，生成 API Key

**OpenAI 兼容接口**：
- 支持任何 OpenAI 格式的 API（OpenAI、MiniMax、DeepSeek 等）
- 获取对应的 API Key 和 Base URL

### 3. 克隆项目

```bash
git clone https://github.com/your-repo/mini-agent-go
cd mini-agent-go
go mod tidy
```

### 4. 配置

编辑 `.agent/config/config.yaml`（或从示例复制）：

```yaml
llm:
  provider: anthropic          # 或 openai
  model: claude-sonnet-4-6
  api_key: sk-ant-...         # 你的 API Key

agent:
  max_steps: 200
  token_limit: 80000
  workspace_dir: .            # Agent 工作目录
```

### 5. 运行！

```bash
go build -o mini-agent-go ./cmd
./mini-agent-go
```

```
╭─────────────────────────────────────────╮
│          Mini Agent                     │
│   你的数字员工已就位，等待指令            │
╰─────────────────────────────────────────╯
> 你好，帮我列出当前目录的文件
```

---

## 项目结构速览

```
mini-agent-go/
├── cmd/                    # 程序入口
├── internal/
│   ├── agent/              # Agent 主循环（第3章）
│   ├── llm/                # LLM 客户端抽象（第1章）
│   ├── tools/              # 工具系统（第2章）
│   ├── gateway/            # IM Gateway（第4章）
│   ├── task/               # 异步任务管理（第5章）
│   ├── schema/             # 统一数据结构
│   ├── config/             # 配置加载
│   └── tui/                # 终端界面（第4章）
├── skills/                 # 技能插件目录（第7章）
└── .agent/                 # 运行时数据目录
    ├── config/             # 配置文件
    ├── logs/               # 执行日志
    ├── memory.json         # 持久记忆
    └── tools/bin/          # 外部工具二进制（第8章）
```

---

## 关于 Claude Code 工作方式

贯穿本教程，你会看到这样的标记：

> 💡 **Claude Code 实现决策**：这里解释了在构建过程中，Claude Code 为什么做出这个设计选择。

这不是普通的"作者注"——它还原了一个真实的人机协作过程：你描述需求，AI 设计实现，你审查确认，AI 继续推进。这套教程本身，也是用这种方式写成的。

---

准备好了吗？让我们从最基础的地方开始——[第一章：与 AI 握手](ch01-llm-api.md)。
