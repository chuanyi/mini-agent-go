# 第八章：自我进化——模型切换与能力扩展

> 最厉害的数字员工不只执行任务，还能自我升级——
> 更换更强大的大脑，习得新技能，连工具都能自己安装。

---

## LLM 切换策略

不同任务对模型能力的需求不同。聪明地选择模型能在效果和成本之间找到最佳平衡：

```
┌─────────────────────────────────────────────────────┐
│               模型选择矩阵                           │
├──────────────────┬─────────────────┬────────────────┤
│ 任务类型         │ 推荐模型         │ 原因           │
├──────────────────┼─────────────────┼────────────────┤
│ 日常对话/问答    │ Haiku 4.5       │ 快速、便宜     │
│ 文件操作/脚本    │ Sonnet 4.6      │ 均衡性价比     │
│ 复杂推理/架构    │ Opus 4.6        │ 最强理解力     │
│ 代码生成/审查    │ Sonnet 4.6      │ 代码能力突出   │
│ 长文档处理       │ Sonnet 4.6      │ 大上下文窗口   │
└──────────────────┴─────────────────┴────────────────┘
```

### 配置切换

修改 `.agent/config/config.yaml` 即可切换：

```yaml
llm:
  provider: anthropic
  model: claude-haiku-4-5-20251001  # 日常任务用 Haiku

# 需要强推理时改为：
  model: claude-opus-4-6            # 或 claude-sonnet-4-6
```

支持 OpenAI 兼容接口，可以无缝对接 DeepSeek、MiniMax、本地 Ollama 等：

```yaml
llm:
  provider: openai
  model: deepseek-chat
  api_base: https://api.deepseek.com/v1
  api_key: sk-...
```

### 多 Agent 不同模型

更进阶的用法：主 Agent 用便宜模型做路由判断，子 Agent 用强模型做复杂任务：

```go
// 伪码：主 Agent 路由，子 Agent 执行

// 主 Agent：Haiku，快速判断任务类型
mainAgent := agent.New(haiku_client, ...)

// 子 Agent：创建时根据任务类型选模型
func TaskRunner(ctx, description) (string, error) {
    model := selectModel(description)  // 简单任务选 Haiku，复杂选 Sonnet
    subAgent := agent.New(model, ...)
    return subAgent.Run(ctx, description)
}
```

---

## 系统能力扩展：bash 自安装

数字员工最强大的地方之一：**它能自己安装所需的工具**。

```
你: 帮我把这个视频压缩一下

AI: 我需要先检查 ffmpeg 是否可用。
    [调用 bash: "ffmpeg -version 2>&1"]

    看来 ffmpeg 没有安装。让我来安装它。
    [调用 bash: "apt-get install -y ffmpeg 2>&1"]

    安装完成！现在来压缩视频。
    [调用 bash: "ffmpeg -i input.mp4 -crf 23 output.mp4"]

    压缩完成！文件从 2.3GB 减小到 156MB，画质损失可忽略。
```

这种"自我装备"能力让 Agent 能完成你没有预先准备的任务。只要有 `bash` 工具和合适的权限，它就能扩展自己的能力边界。

常见的自动安装场景：
- **音视频处理**：ffmpeg、imagemagick
- **数据处理**：jq、csvkit、pandoc
- **开发工具**：特定版本的 linter、formatter
- **网络工具**：curl、wget、httpie

---

## 外部工具：.agent/tools/bin/

除了 bash 动态安装，还有一种方式预置工具——把二进制文件放到 `.agent/tools/bin/`：

```
.agent/tools/bin/
├── rg.exe          # ripgrep（grep 工具使用）
├── gen_image.exe   # 自定义图像生成工具
└── .env            # 环境变量配置
```

### 外部工具加载机制

```go
// 伪码：外部工具加载
// 对应真实代码：internal/tools/external_tool.go

// external_tool_config.json 定义工具接口
{
  "tools": [
    {
      "name": "gen_image",
      "description": "使用 AI 生成图片",
      "parameters": {
        "type": "object",
        "properties": {
          "prompt": {"type": "string", "description": "图片描述"},
          "size": {"type": "string", "description": "尺寸，如 1024x1024"}
        },
        "required": ["prompt"]
      },
      "binary": "gen_image"  // 对应 .agent/tools/bin/gen_image
    }
  ]
}

// ExternalTool 执行：调用二进制，传入 JSON 参数，读取 JSON 输出
func (t *ExternalTool) Execute(ctx, params) (*ToolResult, error) {
    // 序列化参数为 JSON
    argsJSON, _ := json.Marshal(params)

    // 执行二进制
    cmd := exec.CommandContext(ctx, t.binaryPath, string(argsJSON))
    output, err := cmd.Output()

    // 解析 JSON 输出
    var result ExternalToolResult
    json.Unmarshal(output, &result)

    return ContentResult(result.Content), nil
}
```

### 实现自定义外部工具

任何语言都可以实现外部工具，只需遵循协议：

**输入**：命令行参数，JSON 字符串
**输出**：标准输出，JSON 字符串

```python
# 示例：Python 实现的图片生成工具
# 编译/打包后放到 .agent/tools/bin/gen_image

import sys, json

params = json.loads(sys.argv[1])
prompt = params["prompt"]

# 调用图片生成 API
image_url = generate_image(prompt)

print(json.dumps({
    "success": True,
    "content": f"图片已生成：{image_url}",
    "details": f"![生成的图片]({image_url})"
}))
```

---

## MCP 集成：标准化工具扩展

Model Context Protocol（MCP）是 Anthropic 推出的开放标准，让任何第三方服务都可以作为 Agent 工具接入：

```
┌──────────────────────────────────────────────────────┐
│                  MCP 生态                            │
├──────────────────────────────────────────────────────┤
│                                                      │
│  MCP Server 示例：                                   │
│  • GitHub MCP — 操作 Issues/PR                       │
│  • Notion MCP — 读写 Notion 页面                     │
│  • Slack MCP — 发送和读取 Slack 消息                 │
│  • Database MCP — SQL 查询执行                       │
│  • Browser MCP — 浏览器自动化                        │
│                                                      │
│  接入方式：                                          │
│  Agent ──► MCP Client ──► MCP Server ──► 外部服务   │
│                                                      │
└──────────────────────────────────────────────────────┘
```

通过 `mcpotter`（MCP 代理工具），可以将 MCP Server 转换为本框架的外部工具格式，实现无缝集成。

---

## 技能沉淀：让 AI 教 AI

最有趣的进化方式：让 Agent 帮你写 SKILL.md。

```
你: 我发现自己经常需要把 JSON 数据转成 CSV 格式，
    能帮我写一个技能，以后就不用每次重复解释了？

AI: 好的，我来写一个 json-to-csv 技能。

    首先问几个问题：
    1. 你通常处理的 JSON 结构是数组还是嵌套对象？
    2. 有特殊的字段过滤或重命名需求吗？
    3. CSV 需要特定的编码或分隔符吗？

你: 一般是数组，需要过滤掉 _internal 开头的字段，UTF-8 编码，逗号分隔

AI: 明白了，为你生成技能文件：
    [调用 bash: "mkdir -p skills/json-to-csv"]
    [调用 write: "skills/json-to-csv/SKILL.md"]

    写入内容：
    ---
    name: json-to-csv
    description: 将 JSON 数组转换为 CSV，自动过滤 _internal 前缀字段
    allowed-tools: [bash, read, write]
    ---

    # JSON 转 CSV 技能
    ...（完整技能内容）

    重启 Agent 后即可使用 json-to-csv 技能！
```

"让 AI 教 AI" 的闭环：
1. 你描述需求
2. Agent 生成 SKILL.md
3. 重启后 Agent 掌握新技能
4. 下次你只需说"帮我转换这个 JSON"

---

## Custom System Prompt：越用越懂你

通过 `.agent/config/system_prompt.md` 自定义 Agent 的基础行为：

```markdown
# 我的数字员工配置

## 工作原则
- 我是一个全栈开发者，Go 和 Python 是主要语言
- 代码风格：简洁胜于复杂，注释最少化
- 提交规范：英文，使用 feat/fix/docs/refactor 前缀

## 常用路径
- 项目根目录：/home/me/projects/
- 工作笔记：/home/me/notes/
- 临时文件夹：/tmp/agent-work/

## 响应偏好
- 给出答案时，优先给代码而不是文字描述
- 遇到多种方案时，直接推荐一个，不要让我选
- 错误要给出根因，而不只是"发生了错误"

## 特殊工具约定
- 视频处理使用 ffmpeg（已预装）
- 图片生成使用 gen_image 工具
- 发送通知使用 bash 调用 notify.sh 脚本
```

这份文件就是你的"员工手册"，越详细，Agent 的行为越符合你的期望。随着使用，不断完善这份配置——这本身就是一种"进化"。

---

## 能力边界的扩展路径

```
基础能力（开箱即用）
│
├── 工具扩展
│   ├── .agent/tools/bin/ 预置二进制
│   ├── bash 工具动态安装
│   └── MCP Server 接入
│
├── 知识扩展
│   ├── skills/ 添加领域技能
│   ├── record_note 积累工作知识
│   └── system_prompt 精炼行为规范
│
└── 智能升级
    ├── 切换更强的基础模型
    ├── 按任务类型选模型
    └── 子 Agent 按需使用不同模型
```

---

## 模拟对话：完整进化过程

```
[第 1 周]
你: 帮我处理这些图片，压缩并转换格式
AI: [bash: convert -version] ImageMagick 未安装
    [bash: apt-get install imagemagick]
    [bash: convert input.jpg -quality 80 output.jpg]
    完成！

[第 2 周]
你: 帮我设计一个 API 接口
AI: [按一般原则设计，结果还需要你大量修改]

你: 我们的 API 设计有几个固定规范：
    1. 统一使用 RESTful，动词用 HTTP Method
    2. 错误码格式：{code: int, message: str, detail: str}
    3. 所有接口需要版本号 /v1/

AI: 好的，我来写一个"API 设计"技能，以后直接遵循这些规范。
    [创建 skills/api-design/SKILL.md]

[第 3 周，新会话]
你: 帮我设计用户管理的 API
AI: [get_skill: api-design]
    根据我们的 API 设计规范：

    POST /v1/users          创建用户
    GET  /v1/users/{id}     获取用户
    PUT  /v1/users/{id}     更新用户
    DELETE /v1/users/{id}   删除用户

    错误响应格式：
    {"code": 404, "message": "User not found", "detail": "..."}

    （完全符合你的规范，零修改）
```

数字员工在第 2 周把你的规范变成了自己的"技能"，第 3 周开始自动应用，完全不需要再解释。

---

## 本章小结

本章展示了数字员工的自我进化能力：

- **LLM 切换**：按任务选模型（Haiku 日常/Sonnet 均衡/Opus 复杂），支持 OpenAI 兼容接口
- **bash 自安装**：通过 bash 工具动态扩展系统能力（ffmpeg、imagemagick 等）
- **外部工具**：`.agent/tools/bin/` 预置任意语言实现的工具，JSON 协议接入
- **MCP 集成**：标准化第三方服务接入（GitHub、Notion、数据库等）
- **技能沉淀**：让 Agent 帮你写 SKILL.md，"让 AI 教 AI"
- **Custom System Prompt**：精炼行为规范，越用越懂你

对应代码：
- `internal/tools/external_tool.go` — 外部工具加载机制
- `internal/llm/client.go` — 多 Provider 支持
- `internal/config/config.go` — 配置管理
- `.agent/tools/bin/` — 外部工具目录

---

## 下一章预告

你已经拥有了一个真正的数字员工。现在，唯一的限制是你的想象力。

**[第九章：无限可能——应用场景与你的想象](ch09-imagination.md)**
