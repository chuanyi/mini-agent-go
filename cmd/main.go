package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"mini-agent-go/internal/agent"
	"mini-agent-go/internal/config"
	"mini-agent-go/internal/gateway"
	"mini-agent-go/internal/gateway/channels/serverim"
	"mini-agent-go/internal/llm"
	"mini-agent-go/internal/task"
	"mini-agent-go/internal/tools"
	"mini-agent-go/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	serverchan_sdk "github.com/easychen/serverchan-sdk-golang"
)

func main() {
	// Parse command line arguments
	workspaceDir := "./workspace"
	if len(os.Args) > 1 {
		if os.Args[1] == "--help" || os.Args[1] == "-h" {
			printUsage()
			return
		}
		workspaceDir = os.Args[1]
	}

	// Convert workspace to absolute path
	absWorkspaceDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		fmt.Printf("Error: failed to resolve workspace path: %v\n", err)
		os.Exit(1)
	}
	workspaceDir = absWorkspaceDir

	// Ensure workspace directory exists
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		fmt.Printf("Error: failed to create workspace directory: %v\n", err)
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error: failed to load configuration: %v\n", err)
		fmt.Println("\nExpected config location: .agent/config.yaml")
		os.Exit(1)
	}

	// Override workspace directory if provided
	if workspaceDir != "" {
		cfg.Agent.WorkspaceDir = workspaceDir
	}

	// Initialize LLM client
	logDir := filepath.Join(agent.ResolveAgentDir(), "logs")
	llmConfig := llm.Config{
		APIKey:      cfg.LLM.APIKey,
		APIBase:     cfg.LLM.APIBase,
		Model:       cfg.LLM.Model,
		Provider:    cfg.LLM.Provider,
		MaxRetries:  cfg.LLM.Retry.MaxRetries,
		MaxTokens:   cfg.LLM.MaxTokens,
		Temperature: cfg.LLM.Temperature,
		LogDir:      logDir,
	}

	model, err := llm.NewClient(llmConfig)
	if err != nil {
		fmt.Printf("Error: failed to initialize LLM client: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()

	// Load system prompt if available
	systemPrompt := ""
	systemPromptPath, err := config.FindConfigFile(cfg.Agent.SystemPromptPath)
	if err == nil {
		data, err := os.ReadFile(systemPromptPath)
		if err == nil {
			systemPrompt = string(data)
		}
	}
	// If system prompt not found, use a minimal default
	if systemPrompt == "" {
		systemPrompt = "You are 小K (MiniK), a versatile AI assistant capable of executing complex tasks through a rich toolset. Your name comes from the fact that you always like to say \"OK\"."
	}

	// Resolve .agent/tools/ directory (exe dir first, then CWD fallback)
	externalToolsDir := agent.ResolveExternalToolsDir()

	// Load skills if skills directory exists
	var skillLoader *tools.SkillLoader
	var skillTool tools.Tool
	skillsDir := filepath.Join(".agent", "skills")
	if _, err := os.Stat(skillsDir); err == nil {
		tool, loader, err := tools.CreateSkillTools(skillsDir)
		if err != nil {
			// Silent error handling
		} else {
			skillLoader = loader
			skillTool = tool
		}
	}

	// Create TUI Model with all components for proper initialization
	// (this internally creates the Agent with default tools, external tools, and skill tool)
	m := tui.NewModelWithComponents(cfg, model, systemPrompt, skillLoader, skillTool)

	// --- Task/Cron system setup ---
	// Errors are collected via m.AddStartupMessage (not fmt.Printf which is invisible under AltScreen)
	taskDir := filepath.Join(agent.ResolveAgentDir(), "tasks")
	var taskMgr *task.TaskManager
	var cronMgr *task.CronManager
	var cronStore *task.CronStore

	taskStore, err := task.NewTaskStore(taskDir)
	if err != nil {
		m.AddStartupMessage(fmt.Sprintf("⚠️ Task store init failed: %v", err))
	} else {
		// Recover any tasks stuck in "running" from a previous crash
		if err := taskStore.RecoverStaleTasks(); err != nil {
			m.AddStartupMessage(fmt.Sprintf("⚠️ Task recovery failed: %v", err))
		}

		// TaskRunner closure: creates a fresh subagent for each task.
		// The subagent gets all tools EXCEPT task/cron tools (no recursive spawning).
		runner := func(ctx context.Context, description string) (string, error) {
			subagentOptions := []agent.Option{
				agent.WithMaxSteps(cfg.Agent.MaxSteps),
				agent.WithTokenLimit(cfg.Agent.TokenLimit),
				agent.WithWorkspace(cfg.Agent.WorkspaceDir),
				agent.WithSystemPrompt(systemPrompt + "\n\n[You are a subagent executing an async task. Complete the task described below and return a concise result summary.]"),
			}
			if skillLoader != nil {
				subagentOptions = append(subagentOptions, agent.WithSkillLoader(skillLoader))
			}

			sub := agent.New(model, subagentOptions...)
			defer sub.Close()
			sub.RegisterDefaultTools()
			sub.UnregisterTool("todo") // Subagent has no TUI — todo checklist is useless here
			sub.LoadExternalTools(externalToolsDir)
			if skillTool != nil {
				sub.RegisterTool(skillTool)
			}

			return sub.Run(ctx, description)
		}

		taskMgr = task.NewTaskManager(context.Background(), taskStore, runner, cfg.Task.MaxWorkers)
		taskMgr.Start()

		// Set up ServerChan notification on task completion
		if cfg.Notify.Enabled && cfg.Notify.SendKey != "" {
			taskMgr.OnTaskComplete = func(description, result string) {
				title := fmt.Sprintf("任务[%s]完成", description)
				serverchan_sdk.ScSend(cfg.Notify.SendKey, title, result, nil)
			}
		}

		// Cron store (independent — cron failure should NOT block task tools)
		var cronErr error
		cronStore, cronErr = task.NewCronStore(taskDir)
		if cronErr != nil {
			m.AddStartupMessage(fmt.Sprintf("⚠️ Cron store init failed: %v", cronErr))
			cronStore = nil
		} else {
			cronMgr = task.NewCronManager(context.Background(), cronStore, taskMgr)
			cronMgr.Start()
		}
	}

	// Register task/cron tools — FLAT, not nested inside cronStore success block
	// Task tools only need taskMgr; cron tools only need cronStore.
	if taskMgr != nil || cronStore != nil {
		var taskTools []tools.Tool

		if taskMgr != nil {
			taskTools = append(taskTools, tools.NewTaskTool(taskMgr))
		}
		if cronStore != nil {
			taskTools = append(taskTools, tools.NewCronTool(cronStore))
		}

		m.SetTaskTools(taskTools)
	}

	// Configure Web control if enabled
	m.SetWebConfig(cfg.WebControl.Enabled, cfg.WebControl.Host, cfg.WebControl.Port, cfg.WebControl.ShowQRCode)

	// Start IM Gateway if any channels are configured
	var gw *gateway.Gateway
	if len(cfg.Channels) > 0 {
		gw = gateway.New(llmConfig, cfg, systemPrompt, skillLoader, skillTool, externalToolsDir, logDir)
		// Inject task/cron tools into gateway agents (same as TUI main agent)
		if taskMgr != nil || cronStore != nil {
			var gwTaskTools []tools.Tool
			if taskMgr != nil {
				gwTaskTools = append(gwTaskTools, tools.NewTaskTool(taskMgr))
			}
			if cronStore != nil {
				gwTaskTools = append(gwTaskTools, tools.NewCronTool(cronStore))
			}
			gw.SetTaskTools(gwTaskTools)
		}

		gwErr := false
		for _, chCfg := range cfg.Channels {
			var ch gateway.IMChannel
			switch chCfg.Type {
			case "serverim":
				ch = serverim.New(chCfg)
			default:
				m.AddStartupMessage(fmt.Sprintf("⚠️ Unknown channel type: %s", chCfg.Type))
				gwErr = true
				continue
			}
			if err := gw.AddChannel(ch); err != nil {
				m.AddStartupMessage(fmt.Sprintf("⚠️ Gateway channel add failed: %v", err))
				gwErr = true
			}
		}
		if !gwErr {
			if err := gw.Start(); err != nil {
				m.AddStartupMessage(fmt.Sprintf("⚠️ Gateway start failed: %v", err))
				gw = nil
			} else {
				m.AddStartupMessage(fmt.Sprintf("✅ IM Gateway started (%d channel(s))", len(cfg.Channels)))
			}
		}
	}

	// Start Bubbletea program
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(), // Use alternate screen buffer
		tea.WithMouseCellMotion(),
	)

	// Set program reference for Web control
	m.SetProgram(p)

	// Start Web server if enabled
	if cfg.WebControl.Enabled {
		if err := m.StartWebServer(); err != nil {
			// This also gets wiped by AltScreen, but web errors are less critical
		}
	}

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Clean shutdown
	m.Close()
	if gw != nil {
		gw.Stop()
	}
	// Clean shutdown of task/cron managers
	if cronMgr != nil {
		cronMgr.Stop()
	}
	if taskMgr != nil {
		taskMgr.Stop()
	}
}

func printUsage() {
	fmt.Println("MiniK - Golang Version (TUI Mode)")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  mini-agent-go [workspace_directory]")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  mini-agent-go                    # Use ./workspace as workspace")
	fmt.Println("  mini-agent-go /path/to/dir       # Use specific workspace directory")
	fmt.Println()
	fmt.Println("Interactive Commands:")
	fmt.Println("  /help     - Show help")
	fmt.Println("  /clear    - Clear session")
	fmt.Println("  /stats    - Show statistics")
	fmt.Println("  /quit     - Exit")
	fmt.Println()
	fmt.Println("Keyboard Shortcuts:")
	fmt.Println("  Enter         - Submit message")
	fmt.Println("  Shift+Enter   - New line")
	fmt.Println("  Ctrl+C        - Quit")
	fmt.Println()
}
