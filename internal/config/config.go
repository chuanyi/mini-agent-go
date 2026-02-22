package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"mini-agent-go/internal/schema"
)

// RetryConfig represents retry configuration
type RetryConfig struct {
	Enabled         bool    `yaml:"enabled"`
	MaxRetries      int     `yaml:"max_retries"`
	InitialDelay    float64 `yaml:"initial_delay"`
	MaxDelay        float64 `yaml:"max_delay"`
	ExponentialBase float64 `yaml:"exponential_base"`
}

// LLMConfig represents LLM configuration
type LLMConfig struct {
	APIKey      string             `yaml:"api_key"`
	APIBase     string             `yaml:"api_base"`
	Model       string             `yaml:"model"`
	Provider    schema.LLMProvider `yaml:"provider"`
	MaxTokens   int                `yaml:"max_tokens"`   // Max output tokens per request (default: provider-specific)
	Temperature float64            `yaml:"temperature"`  // Sampling temperature (default: -1 = provider default)
	Retry       RetryConfig        `yaml:"retry"`
}

// AgentConfig represents agent configuration
type AgentConfig struct {
	MaxSteps         int    `yaml:"max_steps"`
	WorkspaceDir     string `yaml:"workspace_dir"`
	SystemPromptPath string `yaml:"system_prompt_path"`
	TokenLimit       int    `yaml:"token_limit"`
}

// ToolsConfig represents tools configuration
type ToolsConfig struct {
	EnableFileTools bool   `yaml:"enable_file_tools"`
	EnableBash      bool   `yaml:"enable_bash"`
	EnableNote      bool   `yaml:"enable_note"`
	EnableSkills    bool   `yaml:"enable_skills"`
	SkillsDir       string `yaml:"skills_dir"`
	EnableMCP       bool   `yaml:"enable_mcp"`
	MCPConfigPath   string `yaml:"mcp_config_path"`
}

// TaskConfig represents async task/subagent configuration
type TaskConfig struct {
	MaxWorkers  int `yaml:"max_workers"`   // max concurrent subagent tasks (default: 3)
	TaskTimeout int `yaml:"task_timeout"`  // per-task timeout in seconds (default: 600)
}

// NotifyConfig represents notification configuration
type NotifyConfig struct {
	Enabled bool   `yaml:"enabled"`
	SendKey string `yaml:"sendkey"`
}

// WebControlConfig represents web remote control configuration
type WebControlConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	ShowQRCode bool   `yaml:"show_qrcode"`
}

// ChannelConfig holds configuration for a single IM channel instance.
type ChannelConfig struct {
	Type          string `yaml:"type"`           // "serverim"
	AccountID     string `yaml:"account_id"`     // optional; derived from token prefix if omitted
	Token         string `yaml:"token"`          // required
	Mode          string `yaml:"mode"`           // "webhook" (default) or "polling"
	WebhookAddr   string `yaml:"webhook_addr"`   // listen address for webhook mode, e.g. ":8088"
	WebhookSecret string `yaml:"webhook_secret"` // optional header secret for webhook validation
}

// Config represents the main configuration
type Config struct {
	LLM        LLMConfig        `yaml:"llm"`
	Agent      AgentConfig      `yaml:"agent"`
	Tools      ToolsConfig      `yaml:"tools"`
	Task       TaskConfig       `yaml:"task"`
	Notify     NotifyConfig     `yaml:"notify"`
	WebControl WebControlConfig `yaml:"web_control"`
	Channels   []ChannelConfig  `yaml:"channels"`
}

// Load loads configuration from file
func Load() (*Config, error) {
	configPath := filepath.Join(".agent", "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		return nil, fmt.Errorf("configuration file not found: %s", configPath)
	}

	// Read file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate and set defaults
	if err := validateAndSetDefaults(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// validateAndSetDefaults validates the configuration and sets default values
func validateAndSetDefaults(config *Config) error {
	// Validate API key
	if config.LLM.APIKey == "" {
		return fmt.Errorf("API key is required")
	}

	if config.LLM.APIKey == "YOUR_API_KEY_HERE" {
		return fmt.Errorf("please configure a valid API key")
	}

	// Set LLM defaults
	if config.LLM.APIBase == "" {
		config.LLM.APIBase = "https://api.minimaxi.com/anthropic"
	}

	if config.LLM.Model == "" {
		config.LLM.Model = "MiniMax-M2"
	}

	if config.LLM.Provider == "" {
		config.LLM.Provider = schema.ProviderAnthropic
	}

	// Set LLM generation defaults
	// MaxTokens 0 means "use provider default" (handled in client code)
	// Temperature -1 means "use provider default" (since 0.0 is a valid temperature)
	if config.LLM.Temperature == 0 {
		config.LLM.Temperature = -1
	}

	// Set retry defaults
	if config.LLM.Retry.MaxRetries == 0 {
		config.LLM.Retry.MaxRetries = 3
	}

	if config.LLM.Retry.InitialDelay == 0 {
		config.LLM.Retry.InitialDelay = 1.0
	}

	if config.LLM.Retry.MaxDelay == 0 {
		config.LLM.Retry.MaxDelay = 60.0
	}

	if config.LLM.Retry.ExponentialBase == 0 {
		config.LLM.Retry.ExponentialBase = 2.0
	}

	// Set agent defaults
	if config.Agent.MaxSteps == 0 {
		config.Agent.MaxSteps = 50
	}

	if config.Agent.WorkspaceDir == "" {
		config.Agent.WorkspaceDir = "./workspace"
	}

	if config.Agent.SystemPromptPath == "" {
		config.Agent.SystemPromptPath = "system_prompt.md"
	}

	if config.Agent.TokenLimit == 0 {
		config.Agent.TokenLimit = 80000
	}

	// Set tools defaults
	if config.Tools.SkillsDir == "" {
		config.Tools.SkillsDir = filepath.Join(".agent", "skills")
	}

	if config.Tools.MCPConfigPath == "" {
		config.Tools.MCPConfigPath = "mcp.json"
	}

	// Set task defaults
	if config.Task.MaxWorkers == 0 {
		config.Task.MaxWorkers = 3
	}
	if config.Task.TaskTimeout == 0 {
		config.Task.TaskTimeout = 600
	}

	// Set web control defaults
	if config.WebControl.Host == "" {
		config.WebControl.Host = "0.0.0.0"
	}
	if config.WebControl.Port == 0 {
		config.WebControl.Port = 8765
	}

	return nil
}

// GetDefaultConfigPath returns the default configuration file path
func GetDefaultConfigPath() string {
	return filepath.Join(".agent", "config.yaml")
}

// FindConfigFile searches for a configuration file in .agent/ directory
func FindConfigFile(filename string) (string, error) {
	configPath := filepath.Join(".agent", filename)
	if _, err := os.Stat(configPath); err == nil {
		return configPath, nil
	}
	return "", fmt.Errorf("config file not found: %s", configPath)
}

// Save saves configuration to file
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
