package tools

import (
	"context"
	"fmt"
	"strings"

	"mini-agent-go/internal/task"
)

// CronTool is a unified tool for managing cron schedules.
type CronTool struct {
	BaseTool
	store *task.CronStore
}

func NewCronTool(store *task.CronStore) *CronTool {
	return &CronTool{
		BaseTool: BaseTool{
			NameValue:        "cron",
			DescriptionValue: "Manage scheduled recurring background tasks. Use when user asks for scheduled or periodic execution.",
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"add", "list", "remove"},
						"description": "add=create schedule, list=show schedules, remove=delete schedule",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Short name for the cron job (action=add)",
					},
					"cron_expr": map[string]interface{}{
						"type":        "string",
						"description": "5-field cron expression, e.g. '*/5 * * * *' (action=add)",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Prompt for spawned tasks (action=add)",
					},
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Cron entry ID (action=remove)",
					},
				},
				"required": []string{"action"},
			},
		},
		store: store,
	}
}

func (t *CronTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	action, _ := params["action"].(string)

	switch action {
	case "add":
		return t.add(params)
	case "list":
		return t.list()
	case "remove":
		return t.remove(params)
	default:
		return ErrorResult(fmt.Sprintf("unknown action %q — use add/list/remove", action)), nil
	}
}

func (t *CronTool) add(params map[string]interface{}) (*ToolResult, error) {
	name, _ := params["name"].(string)
	cronExpr, _ := params["cron_expr"].(string)
	desc, _ := params["description"].(string)

	if name == "" || cronExpr == "" || desc == "" {
		return ErrorResult("name, cron_expr, and description are all required for action=add"), nil
	}

	entry, err := t.store.Add(name, cronExpr, desc)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to add cron: %v", err)), nil
	}

	return ContentResult(fmt.Sprintf("Cron created: %s\nName: %s\nExpression: %s\nNext run: %s",
		entry.ID, entry.Name, entry.CronExpr, entry.NextRunAt.Format("2006-01-02 15:04:05"))), nil
}

func (t *CronTool) list() (*ToolResult, error) {
	entries := t.store.List()
	if len(entries) == 0 {
		return ContentResult("No cron schedules configured."), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Cron schedules (%d):\n\n", len(entries)))
	for _, e := range entries {
		enabledStr := "enabled"
		if !e.Enabled {
			enabledStr = "disabled"
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s  expr=%q  %s  next=%s",
			e.ID, e.Name, e.CronExpr, enabledStr, e.NextRunAt.Format("2006-01-02 15:04:05")))
		if e.LastTaskID != "" {
			sb.WriteString(fmt.Sprintf("  last_task=%s", e.LastTaskID))
		}
		sb.WriteString("\n")
	}

	return ContentResult(sb.String()), nil
}

func (t *CronTool) remove(params map[string]interface{}) (*ToolResult, error) {
	id, _ := params["id"].(string)
	if id == "" {
		return ErrorResult("id is required for action=remove"), nil
	}

	if err := t.store.Remove(id); err != nil {
		return ErrorResult(err.Error()), nil
	}

	return ContentResult(fmt.Sprintf("Cron entry %s has been removed.", id)), nil
}
