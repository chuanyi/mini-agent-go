package tools

import (
	"context"
	"fmt"
	"strings"

	"mini-agent-go/internal/task"
)

// TaskTool is a unified tool for managing background tasks (subagent execution).
type TaskTool struct {
	BaseTool
	mgr *task.TaskManager
}

func NewTaskTool(mgr *task.TaskManager) *TaskTool {
	return &TaskTool{
		BaseTool: BaseTool{
			NameValue:        "task",
			DescriptionValue: "Manage background tasks (subagent execution). Use when user asks to run in background, submit a task, or execute asynchronously.",
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"run", "list", "get", "cancel", "delete"},
						"description": "run=create background task, list=show tasks, get=task detail, cancel=stop task, delete=remove finished task",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Prompt for the subagent (action=run)",
					},
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Task ID (action=get/cancel/delete)",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"", "pending", "running", "completed", "failed", "cancelled"},
						"description": "Filter by status (action=list)",
					},
					"all_finished": map[string]interface{}{
						"type":        "boolean",
						"description": "Delete all finished tasks (action=delete)",
					},
				},
				"required": []string{"action"},
			},
		},
		mgr: mgr,
	}
}

func (t *TaskTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	action, _ := params["action"].(string)

	switch action {
	case "run":
		return t.run(params)
	case "list":
		return t.list(params)
	case "get":
		return t.get(params)
	case "cancel":
		return t.cancel(params)
	case "delete":
		return t.delete(params)
	default:
		return ErrorResult(fmt.Sprintf("unknown action %q — use run/list/get/cancel/delete", action)), nil
	}
}

func (t *TaskTool) run(params map[string]interface{}) (*ToolResult, error) {
	desc, _ := params["description"].(string)
	if desc == "" {
		return ErrorResult("description is required for action=run"), nil
	}

	id, err := t.mgr.AddTask(desc, "")
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to create task: %v", err)), nil
	}

	return ContentResult(fmt.Sprintf("Task created: %s\nDescription: %s\nStatus: pending\nThe task is now running in the background. Report the task ID to the user and move on.", id, desc)), nil
}

func (t *TaskTool) list(params map[string]interface{}) (*ToolResult, error) {
	status, _ := params["status"].(string)

	tasks := t.mgr.ListTasks(task.TaskStatus(status))
	if len(tasks) == 0 {
		msg := "No tasks found."
		if status != "" {
			msg = fmt.Sprintf("No tasks with status %q found.", status)
		}
		return ContentResult(msg), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tasks (%d):\n\n", len(tasks)))
	for _, tk := range tasks {
		sb.WriteString(fmt.Sprintf("  [%s] %s  status=%s", tk.ID, truncate(tk.Description, 60), tk.Status))
		if tk.CronID != "" {
			sb.WriteString(fmt.Sprintf("  cron=%s", tk.CronID))
		}
		sb.WriteString(fmt.Sprintf("  created=%s", tk.CreatedAt.Format("15:04:05")))
		sb.WriteString("\n")
	}

	return ContentResult(sb.String()), nil
}

func (t *TaskTool) get(params map[string]interface{}) (*ToolResult, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return ErrorResult("id is required for action=get"), nil
	}

	tk, err := t.mgr.GetTask(id)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task: %s\n", tk.ID))
	sb.WriteString(fmt.Sprintf("Status: %s\n", tk.Status))
	sb.WriteString(fmt.Sprintf("Description: %s\n", tk.Description))
	sb.WriteString(fmt.Sprintf("Created: %s\n", tk.CreatedAt.Format("2006-01-02 15:04:05")))
	if tk.StartedAt != nil {
		sb.WriteString(fmt.Sprintf("Started: %s\n", tk.StartedAt.Format("2006-01-02 15:04:05")))
	}
	if tk.FinishedAt != nil {
		sb.WriteString(fmt.Sprintf("Finished: %s\n", tk.FinishedAt.Format("2006-01-02 15:04:05")))
	}
	if tk.CronID != "" {
		sb.WriteString(fmt.Sprintf("Cron: %s\n", tk.CronID))
	}
	if tk.Result != "" {
		sb.WriteString(fmt.Sprintf("\nResult:\n%s\n", tk.Result))
	}
	if tk.Error != "" {
		sb.WriteString(fmt.Sprintf("\nError:\n%s\n", tk.Error))
	}

	return ContentResult(sb.String()), nil
}

func (t *TaskTool) cancel(params map[string]interface{}) (*ToolResult, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return ErrorResult("id is required for action=cancel"), nil
	}

	if err := t.mgr.CancelTask(id); err != nil {
		return ErrorResult(err.Error()), nil
	}

	return ContentResult(fmt.Sprintf("Task %s has been cancelled.", id)), nil
}

func (t *TaskTool) delete(params map[string]interface{}) (*ToolResult, error) {
	id, _ := params["id"].(string)
	allFinished, _ := params["all_finished"].(bool)

	if id == "" && !allFinished {
		return ErrorResult("provide either 'id' or 'all_finished: true' for action=delete"), nil
	}

	if allFinished {
		count, err := t.mgr.DeleteFinishedTasks()
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to delete finished tasks: %v", err)), nil
		}
		if count == 0 {
			return ContentResult("No finished tasks to delete."), nil
		}
		return ContentResult(fmt.Sprintf("Deleted %d finished task(s).", count)), nil
	}

	if err := t.mgr.DeleteTask(id); err != nil {
		return ErrorResult(err.Error()), nil
	}
	return ContentResult(fmt.Sprintf("Task %s has been deleted.", id)), nil
}

// truncate shortens a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
