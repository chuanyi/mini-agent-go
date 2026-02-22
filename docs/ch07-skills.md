# 第七章：技能树开花——可插拔的技能系统

> 工具是"动词"——ls、read、bash。
> 技能是"方法论"——如何做代码审查、如何整理文档、如何处理视频。
> 技能让数字员工从"执行者"变成"专家"。

---

## 工具 vs 技能：概念辨析

```
┌──────────────────────────────────────────────────────┐
│                工具 (Tool)                           │
│                                                     │
│  • 原子操作                                          │
│  • 硬编码在 Go 代码里                                │
│  • 执行具体动作（读文件、运行命令、发请求）            │
│  • 无领域知识                                        │
│                                                     │
│  例：bash("ffmpeg -i input.mp4 output.mp4")         │
└──────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────┐
│                技能 (Skill)                          │
│                                                     │
│  • 业务流程编排                                      │
│  • 写在 Markdown 文件里（无需重新编译）              │
│  • 描述"如何"完成某类任务                            │
│  • 包含领域知识、最佳实践、检查列表                  │
│                                                     │
│  例：code-review.md → 如何做 Go 代码审查             │
│      - 检查错误处理                                  │
│      - 检查并发安全                                  │
│      - 检查命名规范                                  │
│      - 生成带优先级的报告                            │
└──────────────────────────────────────────────────────┘
```

工具让 AI 有能力执行操作，技能让 AI 知道如何正确地执行一类任务。

---

## SKILL.md 格式

每个技能是一个 Markdown 文件，遵循 Agent Skills Spec 1.0 格式：

```markdown
---
name: code-review
description: 对 Go 代码进行全面审查，检查安全性、性能、规范性，生成带优先级的审查报告
allowed-tools:
  - read
  - grep
  - find
  - bash
  - write
metadata:
  version: "1.0"
  author: "team"
---

# Go 代码审查技能

## 审查流程

### 第一步：了解变更范围
使用 `find` 工具列出所有 .go 文件，使用 `git diff` 了解变更。

### 第二步：安全性检查（高优先级）
用 `grep` 搜索以下模式：
- SQL 拼接（而非参数化查询）：`grep "fmt.Sprintf.*SELECT"`
- 命令注入风险：`grep "exec.Command"`
- 未检查的错误：`grep -E "_ = "`
- 硬编码密钥：`grep -iE "(api_key|password|secret)\s*="`

### 第三步：性能检查（中优先级）
- 在循环内重复创建相同对象
- 不必要的内存分配
- 数据库 N+1 查询

### 第四步：代码规范（低优先级）
- 函数命名遵循 Go 惯例（驼峰式）
- 错误必须用 %w 包裹
- 导出类型必须有注释

### 第五步：生成报告
使用 `write` 工具将报告写入 `code-review-{日期}.md`，格式：

## 代码审查报告
**日期**: {日期}
**审查范围**: {文件列表}

### 高优先级问题
- [问题描述] `文件:行号`
  建议：...

### 中优先级问题
...

### 总结
共发现 X 个问题，建议在合并前修复高优先级问题。
```

---

## 渐进式披露：三级加载

这是技能系统最重要的设计——在需要时才加载详细内容，平时只加载元数据，节省 Token：

```
Level 1 — 元数据（系统启动时注入，消耗约 100 tokens）
────────────────────────────────────────────────────────
## Available Skills

- **code-review**: 对 Go 代码进行全面审查，检查安全性、性能、规范性
- **video-process**: 视频压缩、格式转换、添加字幕
- **doc-gen**: 从代码自动生成 API 文档

Use `get_skill` tool to load full skill instructions when needed.

────────────────────────────────────────────────────────
Level 2 — 完整内容（按需加载，get_skill 触发，约 2000 tokens）

AI 决定用某个技能时，调用 get_skill("code-review")
→ 加载完整的 SKILL.md 内容到上下文

────────────────────────────────────────────────────────
Level 3 — 外部资源（路径自动转换）

SKILL.md 内引用 "./scripts/check.sh"
→ 自动转换为 "/absolute/path/to/skills/code-review/scripts/check.sh"
→ AI 可以直接使用这个绝对路径执行脚本
```

```go
// 伪码：技能系统实现
// 对应真实代码：internal/tools/skill_tool.go

// 启动时：扫描所有 SKILL.md，注入 Level 1 元数据
func (l *SkillLoader) GetSkillsMetadataPrompt() string {
    var sb strings.Builder
    sb.WriteString("## Available Skills\n\n")
    for _, skill := range l.loadedSkills {
        sb.WriteString(fmt.Sprintf("- **%s**: %s\n", skill.Name, skill.Description))
    }
    sb.WriteString("\nUse `get_skill` tool to load full skill instructions when needed.")
    return sb.String()
}

// get_skill 工具：Level 2，按需加载完整内容
func (t *GetSkillTool) Execute(ctx, params) (*ToolResult, error) {
    skillName := params["skill_name"].(string)

    skill, err := t.loader.LoadSkill(skillName)
    if err != nil {
        return ErrorResult(fmt.Sprintf("技能 %s 未找到", skillName)), nil
    }

    // Level 3：转换相对路径为绝对路径
    content := convertRelativePaths(skill.Content, skill.SkillPath)

    return ContentResult(skill.ToPrompt()), nil
}
```

---

## SKILL.md 解析流程

```go
// 伪码：SKILL.md 解析
// 对应真实代码：internal/tools/skill_tool.go:LoadSkill()

func (l *SkillLoader) LoadSkill(skillPath string) (*Skill, error) {
    content := os.ReadFile(skillPath)

    // 解析 YAML Frontmatter（--- 包围的部分）
    frontmatterRegex := `(?s)^---\r?\n(.*?)\r?\n---\r?\n(.*)`
    matches := regex.FindSubmatch(content)

    frontmatter := matches[1]  // YAML 部分
    skillContent := matches[2] // Markdown 内容部分

    // 解析 YAML
    var skill Skill
    yaml.Unmarshal(frontmatter, &skill)  // 填充 Name, Description 等字段

    // 转换相对路径
    skillDir := filepath.Dir(skillPath)
    skill.Content = convertRelativePaths(skillContent, skillDir)
    skill.SkillPath = skillPath

    return &skill, nil
}
```

---

## 技能目录结构

```
skills/
├── code-review/
│   ├── SKILL.md           # 技能定义
│   └── scripts/
│       └── check.sh       # 技能使用的辅助脚本（路径自动转换）
├── video-process/
│   ├── SKILL.md
│   └── templates/
│       └── subtitle.ass   # 字幕模板
└── doc-gen/
    └── SKILL.md
```

一个技能就是一个目录，`SKILL.md` 是入口，其他文件是辅助资源。

---

## 三大技能方向

### 1. 办公自动化

```markdown
---
name: weekly-report
description: 从工作记录生成周报，整合日志、提交记录、任务完成情况
allowed-tools: [bash, read, write, recall_note]
---

# 周报生成技能

## 数据收集

1. 查询本周 Git 提交：
   `git log --since="7 days ago" --pretty=format:"%h %s"`

2. 读取任务完成记录：
   使用 recall_note 搜索本周完成的任务

3. 统计代码变更：
   `git diff HEAD~7 --stat`

## 周报格式

### 本周完成
- 功能开发：...
- Bug 修复：...
- 其他：...

### 下周计划
...
```

### 2. 创意自动化（视频处理）

```markdown
---
name: video-compress
description: 视频压缩与格式转换，自动选择最优参数
allowed-tools: [bash, ls]
---

# 视频压缩技能

## 前提检查

首先验证 ffmpeg 是否可用：
```bash
ffmpeg -version
```
如果不存在，先安装：`bash("apt-get install -y ffmpeg")`

## 压缩策略

### 社交媒体（Instagram/抖音）
目标：< 50MB，720p
```bash
ffmpeg -i {input} -vf scale=-1:720 -crf 28 -preset medium {output}
```

### 存档（高质量）
目标：减小 50% 体积，保持原分辨率
```bash
ffmpeg -i {input} -crf 23 -preset slow {output}
```

## 完成后报告压缩比和文件大小变化
```

### 3. 开发自动化（Claude Code 风格）

```markdown
---
name: bug-fix
description: 系统化的 Bug 修复流程：复现、定位根因、修复、验证
allowed-tools: [read, grep, edit, bash, write]
---

# Bug 修复技能

## 步骤 1：复现问题
理解 Bug 描述，用 bash 工具尝试复现问题。
记录：能否稳定复现？错误信息？

## 步骤 2：定位根因
- 使用 grep 搜索相关函数调用
- 使用 read 阅读相关代码文件
- 追踪调用链，找到问题根源

## 步骤 3：制定修复方案
在修改代码前，明确说明：
- 根因是什么
- 修复思路
- 可能的副作用

## 步骤 4：实施修复
使用 edit 工具精确修改，避免引入不相关变更。

## 步骤 5：验证
运行相关测试，确认修复有效且没有回归。
```

---

## 手把手：写你的第一个技能

假设你经常需要做"将中文文档翻译成英文并调整格式"的任务：

**第一步：创建目录**

```bash
mkdir -p skills/translate-doc
```

**第二步：编写 SKILL.md**

```markdown
---
name: translate-doc
description: 将中文技术文档翻译成英文，保持 Markdown 格式和技术术语准确性
allowed-tools:
  - read
  - write
  - edit
metadata:
  version: "1.0"
---

# 文档翻译技能

## 翻译原则

1. **技术术语保留英文**：API、Token、Agent 等不翻译
2. **代码块不翻译**：代码、命令、文件路径保持原样
3. **语气自然**：不要逐字直译，要符合英文技术写作习惯
4. **格式保持**：Markdown 标题层级、列表结构、代码块完全保留

## 执行步骤

1. 用 `read` 工具读取原始文档
2. 将文档分段翻译（每次不超过 1000 字）
3. 用 `write` 工具将译文写入 `{原文件名}-en.md`
4. 自检：技术术语是否准确？格式是否正确？

## 质量检查

完成后重新读取译文，检查：
- [ ] 所有代码块是否保持原样
- [ ] 技术术语是否正确
- [ ] Markdown 格式是否完整
```

**第三步：重启 Agent，技能自动加载**

```
你: 帮我翻译 docs/architecture.md 这份文档

AI: [查看 Available Skills，发现 translate-doc]
    [调用 get_skill "translate-doc"]
    [根据技能指南执行翻译流程]
    ...
```

---

## 模拟对话：技能驱动的代码审查

```
你: 帮我做一次代码审查，重点看 internal/tools/ 目录

AI: 好的，我来进行代码审查。
    [调用 get_skill "code-review"]

    [读取完整技能内容后开始执行...]

    [调用 find: internal/tools/ *.go]
    找到 15 个 Go 文件

    [调用 grep: "fmt.Sprintf.*exec" in internal/tools/]
    bash_tool.go:67: 发现潜在命令注入点

    [调用 grep: "_ = " in internal/tools/]
    external_tool.go:43: 发现被忽略的错误

    [调用 read: bash_tool.go 57:85]
    仔细分析上下文...

    [调用 write: code-review-2026-02-22.md]

AI: ## 代码审查报告
    **日期**: 2026-02-22
    **范围**: internal/tools/（15 个文件）

    ### 高优先级
    1. **bash_tool.go:67** — 命令参数未经转义直接拼接
       建议：使用 `exec.Command` 的参数列表形式而非 shell 字符串

    ### 中优先级
    2. **external_tool.go:43** — 错误被丢弃（`_ = err`）
       建议：至少记录到日志

    ### 低优先级
    3. 部分导出函数缺少 godoc 注释

    共发现 3 处问题。完整报告已保存至 code-review-2026-02-22.md
```

---

## 本章小结

本章实现了可插拔的技能系统：

- **工具 vs 技能**：工具是原子操作，技能是领域方法论
- **SKILL.md 格式**：YAML Frontmatter（name/description/allowed-tools）+ Markdown 内容
- **三级渐进式披露**：元数据注入（Level 1）→ 按需加载（Level 2）→ 路径自动转换（Level 3）
- **技能目录**：独立目录，支持辅助脚本和资源文件
- **三大方向**：办公自动化、创意自动化、开发自动化
- **无需重新编译**：添加新技能只需创建 SKILL.md，重启 Agent 即可

对应代码：
- `internal/tools/skill_tool.go` — SkillLoader、get_skill 工具实现
- `skills/` — 技能目录（你的扩展空间）

---

## 下一章预告

最厉害的数字员工不只执行任务，还能自我升级——更换更强大的大脑，学会新技能，积累专属知识库。

**[第八章：自我进化——模型切换与能力扩展](ch08-evolution.md)**
