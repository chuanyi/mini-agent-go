package gateway

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mini-agent-go/internal/agent"
	"mini-agent-go/internal/config"
	"mini-agent-go/internal/llm"
	"mini-agent-go/internal/tools"
)

const (
	inboundBuf  = 64
	outboundBuf = 64
	workerBuf   = 8
)

// LogSetter is an optional interface that IMChannel implementations may satisfy
// to receive a file-based logger from the Gateway.
type LogSetter interface {
	SetLogger(logger *log.Logger)
}

// agentWorker pairs a dedicated agent with the message queue for one channel instance.
type agentWorker struct {
	msgCh chan *GatewayMessage
	outCh chan<- *GatewayMessage
	ag    *agent.Agent
}

func (w *agentWorker) run(ctx context.Context) {
	defer w.ag.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-w.msgCh:
			if !ok {
				return
			}
			var reply string
			if strings.TrimSpace(msg.Content) == "/clear" {
				w.ag.ClearHistory()
				reply = "会话已重置"
			} else {
				var err error
				reply, err = w.ag.Run(ctx, msg.Content)
				if err != nil {
					reply = fmt.Sprintf("(error: %v)", err)
				}
			}
			select {
			case w.outCh <- &GatewayMessage{
				ChannelType: msg.ChannelType,
				AccountID:   msg.AccountID,
				SenderID:    msg.SenderID,
				Content:     reply,
			}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// Gateway routes inbound IM messages to per-channel agent workers and sends replies back.
type Gateway struct {
	inbound  chan *GatewayMessage
	outbound chan *GatewayMessage
	channels map[string]IMChannel
	workers  map[string]*agentWorker
	ctx      context.Context
	cancel   context.CancelFunc
	logger   *log.Logger
	logFile  *os.File

	// dependencies for creating agent workers
	llmConfig       llm.Config
	cfg             *config.Config
	systemPrompt    string
	skillLoader     *tools.SkillLoader
	skillTool       tools.Tool
	externalToolDir string
	taskTools       []tools.Tool // task/cron tools injected from cmd/main.go
}

// New creates a Gateway. Call AddChannel to register channels before Start.
// logDir is the directory where gateway.log will be written (e.g. ".agent/logs").
func New(
	llmConfig llm.Config,
	cfg *config.Config,
	systemPrompt string,
	skillLoader *tools.SkillLoader,
	skillTool tools.Tool,
	externalToolDir string,
	logDir string,
) *Gateway {
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gateway{
		inbound:         make(chan *GatewayMessage, inboundBuf),
		outbound:        make(chan *GatewayMessage, outboundBuf),
		channels:        make(map[string]IMChannel),
		workers:         make(map[string]*agentWorker),
		ctx:             ctx,
		cancel:          cancel,
		llmConfig:       llmConfig,
		cfg:             cfg,
		systemPrompt:    systemPrompt,
		skillLoader:     skillLoader,
		skillTool:       skillTool,
		externalToolDir: externalToolDir,
	}
	g.logger, g.logFile = newFileLogger(logDir)
	return g
}

// newFileLogger opens (or creates) a timestamped log file and returns a Logger writing to it.
// Falls back to discarding logs if the file cannot be created.
func newFileLogger(logDir string) (*log.Logger, *os.File) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return log.New(os.Stderr, "[gateway] ", log.LstdFlags), nil
	}
	timestamp := time.Now().Format("20060102_150405")
	path := filepath.Join(logDir, fmt.Sprintf("gateway_%s.log", timestamp))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return log.New(os.Stderr, "[gateway] ", log.LstdFlags), nil
	}
	return log.New(f, "", log.LstdFlags), f
}

// SetTaskTools sets task/cron tools to be registered on every agent worker.
// Must be called before AddChannel.
func (g *Gateway) SetTaskTools(taskTools []tools.Tool) {
	g.taskTools = taskTools
}

// AddChannel registers an IMChannel with the gateway. Must be called before Start.
func (g *Gateway) AddChannel(ch IMChannel) error {
	k := key(ch.Type(), ch.AccountID())
	g.channels[k] = ch

	// Inject file logger into channel if it supports it
	if ls, ok := ch.(LogSetter); ok {
		ls.SetLogger(g.logger)
	}

	worker, err := g.newWorker()
	if err != nil {
		return fmt.Errorf("worker for %s: %w", k, err)
	}
	g.workers[k] = worker
	return nil
}

// Start launches all channel listeners, agent workers, and routing goroutines.
func (g *Gateway) Start() error {
	// Start channel listeners
	for _, ch := range g.channels {
		chCopy := ch
		go func() {
			if err := chCopy.Start(g.ctx, g.inbound); err != nil && g.ctx.Err() == nil {
				g.logger.Printf("[gateway] channel %s/%s stopped: %v", chCopy.Type(), chCopy.AccountID(), err)
			}
		}()
	}

	// Start agent workers
	for _, w := range g.workers {
		wCopy := w
		go wCopy.run(g.ctx)
	}

	go g.dispatchLoop()
	go g.sendLoop()

	return nil
}

// Stop cancels the context, shuts down all channels, and closes the log file.
func (g *Gateway) Stop() {
	g.cancel()
	for _, ch := range g.channels {
		_ = ch.Stop()
	}
	if g.logFile != nil {
		g.logFile.Close()
	}
}

// dispatchLoop reads from inbound and routes each message to the right worker.
func (g *Gateway) dispatchLoop() {
	for {
		select {
		case <-g.ctx.Done():
			return
		case msg, ok := <-g.inbound:
			if !ok {
				return
			}
			k := key(msg.ChannelType, msg.AccountID)
			w, found := g.workers[k]
			if !found {
				g.logger.Printf("[gateway] no worker for key %q, dropping message", k)
				continue
			}
			select {
			case w.msgCh <- msg:
			case <-g.ctx.Done():
				return
			}
		}
	}
}

// sendLoop reads from outbound and delivers replies via the appropriate channel.
func (g *Gateway) sendLoop() {
	for {
		select {
		case <-g.ctx.Done():
			return
		case msg, ok := <-g.outbound:
			if !ok {
				return
			}
			k := key(msg.ChannelType, msg.AccountID)
			ch, found := g.channels[k]
			if !found {
				g.logger.Printf("[gateway] no channel for key %q, dropping reply", k)
				continue
			}
			if err := ch.Send(g.ctx, msg); err != nil {
				g.logger.Printf("[gateway] send error on %q: %v", k, err)
			}
		}
	}
}

// newWorker creates an agent and wraps it in an agentWorker.
func (g *Gateway) newWorker() (*agentWorker, error) {
	model, err := llm.NewClient(g.llmConfig)
	if err != nil {
		return nil, err
	}

	opts := []agent.Option{
		agent.WithMaxSteps(g.cfg.Agent.MaxSteps),
		agent.WithTokenLimit(g.cfg.Agent.TokenLimit),
		agent.WithWorkspace(g.cfg.Agent.WorkspaceDir),
		agent.WithSystemPrompt(g.systemPrompt + "\n\n[You are an IM bot agent. Respond concisely and helpfully.]"),
	}
	if g.skillLoader != nil {
		opts = append(opts, agent.WithSkillLoader(g.skillLoader))
	}

	ag := agent.New(model, opts...)
	ag.RegisterDefaultTools()
	ag.UnregisterTool("todo")
	ag.LoadExternalTools(g.externalToolDir)
	if g.skillTool != nil {
		ag.RegisterTool(g.skillTool)
	}
	for _, t := range g.taskTools {
		ag.RegisterTool(t)
	}

	return &agentWorker{
		msgCh: make(chan *GatewayMessage, workerBuf),
		outCh: g.outbound,
		ag:    ag,
	}, nil
}
