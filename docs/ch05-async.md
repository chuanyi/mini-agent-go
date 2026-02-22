# 第五章：分身有术——子 Agent 与异步任务

> 数字员工已经很能干了，但它一次只能做一件事。
> 如果你有 100 个文件要处理呢？如果你希望每天自动生成报告呢？
> 这章教它"分身"。

---

## 为什么需要异步任务？

**场景一**：你给 Agent 布置了一个大任务——"分析这 50 个代码文件，给每个生成文档"。你不可能等着它一个个做完，你有其他事要干。

**场景二**：你希望每天早上 9 点自动生成一份市场简报，不需要你每天手动触发。

**场景三**：你通过手机 IM 下了指令，Agent 开始处理，你去忙别的了。处理完后主动通知你。

这些都需要**异步任务系统**。

---

## 子 Agent：独立运行的 Agent 实例

每个异步任务都由一个**独立的 Agent 实例**执行。为什么不复用主 Agent？

**状态隔离**：任务 A 和任务 B 的对话历史互不干扰。
**并发安全**：多个任务并发运行，没有共享状态竞争。
**工具限制**：子 Agent 不注册 `task` 工具——防止子任务再创建子任务，避免无限嵌套。

```go
// 伪码：子 Agent 创建
// 对应真实代码：cmd/main.go TaskRunner 函数

func createSubAgent(description string) (string, error) {
    // 创建独立的 LLM 客户端
    model, _ := llm.NewClient(llmConfig)

    // 创建子 Agent（不注册 task/cron 工具！）
    ag := agent.New(model,
        agent.WithMaxSteps(cfg.Agent.MaxSteps),
        agent.WithWorkspace(cfg.Agent.WorkspaceDir),
        agent.WithSystemPrompt(systemPrompt),
    )
    ag.RegisterDefaultTools()  // 注册文件、bash、网络工具
    // 注意：没有 ag.RegisterTool(taskTool)
    // 子 Agent 不能创建新任务，防止无限递归

    result, err := ag.Run(ctx, description)
    ag.Close()
    return result, err
}
```

---

## Task 数据结构

每个任务是一个持久化的状态机：

```go
// 伪码：Task 结构
// 对应真实代码：internal/task/task.go

type Task struct {
    ID          string      // 唯一 ID（如 "task-001"）
    Description string      // 任务描述（就是给 Agent 的 prompt）
    Status      TaskStatus  // 状态机
    CronID      string      // 关联的 Cron 任务 ID（可选）
    Result      string      // 执行结果
    Error       string      // 错误信息（失败时）
    CreatedAt   time.Time
    StartedAt   *time.Time
    CompletedAt *time.Time
}

// 状态机：pending → running → completed/failed/cancelled
type TaskStatus string
const (
    StatusPending   TaskStatus = "pending"
    StatusRunning   TaskStatus = "running"
    StatusCompleted TaskStatus = "completed"
    StatusFailed    TaskStatus = "failed"
    StatusCancelled TaskStatus = "cancelled"
)
```

```
状态流转图：

  pending ──────► running ──────► completed
     │                │
     │                └──────────► failed
     │
     └──────────────────────────► cancelled
```

任务持久化到 `.agent/tasks/tasks.json`，即使程序重启，未完成的任务也不会丢失（但 running 状态的任务需要重新触发）。

---

## TaskManager：工作池调度

```go
// 伪码：TaskManager 核心
// 对应真实代码：internal/task/manager.go

type TaskManager struct {
    store      *TaskStore     // 持久化存储
    runner     TaskRunner     // 实际执行函数（就是上面的 createSubAgent）
    maxWorkers int            // 最大并发数（默认 3）
    sem        chan struct{}   // 信号量，控制并发
}

// Start 启动调度器 goroutine
func (m *TaskManager) Start() {
    go m.dispatcher()  // 后台持续监听新任务
}

// dispatcher 调度循环：有新任务时分配 Worker
func (m *TaskManager) dispatcher() {
    for {
        select {
        case <-m.ctx.Done():
            return
        case <-m.notify:           // 收到新任务通知
            m.dispatchPending()    // 处理所有待处理任务
        }
    }
}

// dispatchPending 从 pending 队列取任务，分配给 Worker
func (m *TaskManager) dispatchPending() {
    for _, task := range m.store.ListPending() {
        m.sem <- struct{}{}    // 获取 Worker 槽位（阻塞，超过 maxWorkers 时等待）
        go m.runTask(task.ID, task.Description)
    }
}

// runTask 在 Worker goroutine 中执行单个任务
func (m *TaskManager) runTask(id, description string) {
    defer func() {
        <-m.sem  // 释放 Worker 槽位
    }()

    m.store.MarkRunning(id)

    result, err := m.runner(ctx, description)  // 调用 createSubAgent

    if err != nil {
        m.store.MarkFailed(id, err)
        return
    }

    m.store.MarkCompleted(id, result)

    // 任务完成回调（用于推送通知）
    if m.OnTaskComplete != nil {
        go m.OnTaskComplete(description, result)
    }
}
```

**工作池信号量机制**：

```
maxWorkers = 3

任务队列：[T1, T2, T3, T4, T5]

t=0: T1 开始（sem: 1/3）
t=0: T2 开始（sem: 2/3）
t=0: T3 开始（sem: 3/3）  ← 满了！
t=0: T4 等待...
t=0: T5 等待...

t=30: T1 完成（sem: 2/3）
t=30: T4 立即开始

以此类推，始终最多 3 个并发
```

---

## task 工具：从对话中创建任务

AI 可以通过 `task` 工具创建异步任务：

```go
// 伪码：task 工具
// 对应真实代码：internal/tools/task_tool.go

// task 工具支持的操作（action 参数）：
// "create"  → 创建新任务
// "list"    → 查看任务列表
// "get"     → 查看单个任务详情
// "cancel"  → 取消任务
// "delete"  → 删除已完成任务

func (t *TaskTool) Execute(ctx, params) (*ToolResult, error) {
    action := params["action"].(string)

    switch action {
    case "create":
        description := params["description"].(string)
        id, _ := t.manager.AddTask(description, "")
        return ContentResult(fmt.Sprintf("任务已创建，ID: %s", id)), nil

    case "list":
        tasks := t.manager.ListTasks("")  // 所有状态
        return ContentResult(formatTaskList(tasks)), nil

    case "get":
        id := params["task_id"].(string)
        task, _ := t.manager.GetTask(id)
        return ContentResult(formatTask(task)), nil
    }
}
```

---

## Cron：定时任务

定时任务基于标准 Cron 表达式：

```go
// 伪码：Cron 管理
// 对应真实代码：internal/task/cron.go

type CronManager struct {
    jobs     map[string]*CronJob  // ID → 定时任务
    taskMgr  *TaskManager
}

type CronJob struct {
    ID          string   // 如 "cron-001"
    Schedule    string   // Cron 表达式，如 "0 9 * * *"（每天早9点）
    Description string   // 触发时给 Agent 的 prompt
    Enabled     bool
}

// 每分钟检查一次，到时间就创建 Task
func (c *CronManager) tick() {
    now := time.Now()
    for _, job := range c.jobs {
        if !job.Enabled {
            continue
        }
        if cronMatches(job.Schedule, now) {
            // 创建一个新的异步任务
            c.taskMgr.AddTask(job.Description, job.ID)
        }
    }
}
```

**Cron 表达式快速参考**：

```
┌──────── 分钟 (0-59)
│ ┌────── 小时 (0-23)
│ │ ┌──── 日 (1-31)
│ │ │ ┌── 月 (1-12)
│ │ │ │ ┌ 周 (0-7, 0和7都是周日)
│ │ │ │ │
0 9 * * *    → 每天早上 9:00
0 */2 * * *  → 每 2 小时
0 9 * * 1    → 每周一早 9:00
*/5 * * * *  → 每 5 分钟
```

---

## cron 工具：从对话中管理定时任务

```go
// 伪码：cron 工具操作
// 对应真实代码：internal/tools/cron_tool.go

// 支持操作：create, list, enable, disable, delete

case "create":
    schedule    := params["schedule"].(string)     // Cron 表达式
    description := params["description"].(string)  // 任务内容
    id, _ := c.cronMgr.AddJob(schedule, description)
    return ContentResult(fmt.Sprintf("定时任务已创建，ID: %s\n计划: %s", id, schedule)), nil
```

---

## 任务完成通知

任务完成时自动推送通知：

```go
// 伪码：任务完成回调
// 对应真实代码：cmd/main.go

taskManager.OnTaskComplete = func(description, result string) {
    // 截取结果摘要（前 200 字）
    summary := truncate(result, 200)
    title := "任务完成：" + truncate(description, 30)

    // 推送到 Server 酱（到微信）
    notify.ServerChan(title, summary)
}
```

---

## 模拟对话：异步任务全流程

```
你: 每天早上 9 点帮我生成一份今日科技新闻摘要，保存到 news/ 目录

AI: 好的！我来创建一个定时任务。
    [调用 cron 工具]
    参数：{
      "action": "create",
      "schedule": "0 9 * * *",
      "description": "搜索今日科技新闻，整理成摘要，
                      保存到 news/YYYY-MM-DD.md"
    }

AI: 定时任务已创建（ID: cron-001）。
    每天早上 9:00 将自动执行，完成后推送通知到你的手机。

    你可以随时查看任务状态：
    发送"任务列表"查看所有任务

─────────────────────────────────────────
[第二天早上 9:00 — 后台自动触发]

TaskManager: 创建任务 task-042（来自 cron-001）
SubAgent:    搜索科技新闻...
             [调用 web_fetch]
             整理摘要...
             [调用 write: news/2026-02-23.md]
TaskManager: 任务完成，触发通知回调

[你的手机 — 微信推送通知]
数字员工: 任务完成：每日科技新闻摘要
          今日摘要已生成并保存至 news/2026-02-23.md
          主要新闻：AI 模型再次突破... 量子计算...
─────────────────────────────────────────

你（发消息）: 任务列表

AI: 当前任务状态：
    定时任务:
    ✅ cron-001 | 每天09:00 | 科技新闻摘要 | 已启用

    最近任务:
    ✅ task-042 | 完成 | 科技新闻摘要(2/23)
    ✅ task-041 | 完成 | 科技新闻摘要(2/22)

你: 暂停这个定时任务，我出去旅游一周

AI: [调用 cron 工具 disable: cron-001]
    好的，定时任务 cron-001 已暂停。
    一周后发消息"恢复新闻任务"重新启用。
```

---

## 本章小结

本章让 Agent 具备了"分身"能力：

- **子 Agent**：每个任务独立的 Agent 实例，状态隔离，工具受限（不能递归创建任务）
- **Task 状态机**：pending → running → completed/failed/cancelled，持久化到 JSON
- **TaskManager**：工作池（Worker Pool）调度，信号量控制最大并发数（默认 3）
- **Cron 定时任务**：标准 Cron 表达式，定时触发 Task 创建
- **任务完成通知**：通过回调函数接入 Server 酱推送
- **task/cron 工具**：AI 可以在对话中直接创建和管理任务

对应代码：
- `internal/task/task.go` — Task 数据结构和状态机
- `internal/task/manager.go` — TaskManager 工作池
- `internal/task/cron.go` — Cron 调度
- `internal/tools/task_tool.go` — task 工具
- `internal/tools/cron_tool.go` — cron 工具

---

## 下一章预告

任务跑完了就忘了？真正了解你的助手会记住你的偏好、习惯、重要信息。

**[第六章：记忆宫殿——持久内存与心跳监控](ch06-memory.md)**
