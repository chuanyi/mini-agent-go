package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"nhooyr.io/websocket"
)

// WebState 共享的 Web 状态（指针类型，所有 Model 副本共享）
type WebState struct {
	conn       *websocket.Conn // 唯一 Web 连接（nil=无连接）
	mutex      sync.Mutex
	enabled    bool         // 是否启用 Web 控制
	host       string       // Web 服务器主机
	port       int          // Web 服务器端口
	program    *tea.Program // Bubbletea 程序引用
	showQRCode bool         // 启动时是否显示 QR code
	secret     string       // 访问密钥（每次启动随机生成）
}

// WebMessage represents a message from Web client
type WebMessage struct {
	Type    string `json:"type"`    // "input", "command"
	Content string `json:"content"` // For input: message content; for command: command name
}

// WebEvent represents an event sent to Web client
type WebEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// WebSnapshot represents the full state snapshot sent on connection
type WebSnapshot struct {
	Messages  []WebMessageData `json:"messages"`
	Todos     []WebTodoData    `json:"todos"`
	IsRunning bool             `json:"isRunning"`
	Status    string           `json:"status"`
}

// WebMessageData represents a message in Web format
type WebMessageData struct {
	Type      string                 `json:"type"` // "user", "assistant", "thinking", "tool_call", "tool_result", "system"
	Content   string                 `json:"content"`
	ToolName  string                 `json:"toolName,omitempty"`
	ToolArgs  map[string]interface{} `json:"toolArgs,omitempty"`
	Success   bool                   `json:"success,omitempty"`
	Timestamp string                 `json:"timestamp"`
}

// WebTodoData represents a todo item in Web format
type WebTodoData struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"` // "pending", "in_progress", "completed"
}

// WebFileItem represents a file or directory entry
type WebFileItem struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

// WebFileListData represents directory listing response
type WebFileListData struct {
	Path  string        `json:"path"`
	Files []WebFileItem `json:"files"`
}


// WebFileUploadResult represents file upload result
type WebFileUploadResult struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Error   string `json:"error,omitempty"`
}

// WebFileDeleteResult represents file delete result
type WebFileDeleteResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// SetProgram sets the tea.Program reference for sending messages
func (m *Model) SetProgram(p *tea.Program) {
	if m.web != nil {
		m.web.program = p
	}
}

// SetWebConfig configures web control settings
func (m *Model) SetWebConfig(enabled bool, host string, port int, showQRCode bool) {
	if m.web != nil {
		m.web.enabled = enabled
		m.web.host = host
		m.web.port = port
		m.web.showQRCode = showQRCode
		// Generate a random secret for this session
		if enabled {
			m.web.secret = generateSecret()
		}
	}
}

// generateSecret generates a 32-character random hex string
func generateSecret() string {
	bytes := make([]byte, 16) // 16 bytes = 32 hex characters
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a less secure but still unique value
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// validateSecret checks if the provided secret matches the expected secret
func (m *Model) validateSecret(r *http.Request) bool {
	if m.web == nil || m.web.secret == "" {
		return false
	}
	secret := r.URL.Query().Get("secret")
	return secret == m.web.secret
}

// ShouldShowQRCodeOnStart returns whether to show QR code when TUI starts
func (m *Model) ShouldShowQRCodeOnStart() bool {
	return m.web != nil && m.web.enabled && m.web.showQRCode
}

// StartWebServer starts the WebSocket server for remote control
func (m *Model) StartWebServer() error {
	if m.web == nil || !m.web.enabled {
		return nil
	}

	addr := fmt.Sprintf("0.0.0.0:%d", m.web.port)

	mux := http.NewServeMux()

	// Serve static HTML page
	mux.HandleFunc("/", m.handleIndex)

	// WebSocket endpoint
	mux.HandleFunc("/ws", m.handleWebSocket)

	// File API endpoints (HTTP)
	mux.HandleFunc("/api/files", m.handleAPIFileList)
	mux.HandleFunc("/api/files/content", m.handleAPIFileContent)
	mux.HandleFunc("/api/files/upload", m.handleAPIFileUpload)
	mux.HandleFunc("/api/files/delete", m.handleAPIFileDelete)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start web server: %w", err)
	}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			// Log error but don't crash
			fmt.Printf("Web server error: %v\n", err)
		}
	}()

	return nil
}

// GetWebURL returns the Web control URL with secret parameter
func (m *Model) GetWebURL() string {
	if m.web == nil || !m.web.enabled {
		return ""
	}
	// Try to get local IP for LAN access
	host := m.web.host
	if host == "0.0.0.0" {
		if ip := getLocalIP(); ip != "" {
			host = ip
		} else {
			host = "localhost"
		}
	}
	return fmt.Sprintf("http://%s:%d?secret=%s", host, m.web.port, m.web.secret)
}

// getLocalIP returns the local IP address
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

// handleIndex serves the main HTML page
func (m *Model) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Validate secret
	if !m.validateSecret(r) {
		http.Error(w, "Unauthorized: invalid or missing secret", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(webPageHTML))
}

// handleWebSocket handles WebSocket connections
func (m *Model) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if m.web == nil {
		http.Error(w, "Web control not initialized", http.StatusInternalServerError)
		return
	}

	// Validate secret
	if !m.validateSecret(r) {
		http.Error(w, "Unauthorized: invalid or missing secret", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // Allow all origins for local use
	})
	if err != nil {
		return
	}

	// One-to-one control: new connection replaces old one
	m.web.mutex.Lock()
	oldConn := m.web.conn
	m.web.conn = conn
	m.web.mutex.Unlock()

	// Close old connection if exists
	if oldConn != nil {
		oldConn.Close(websocket.StatusGoingAway, "replaced by new connection")
	}

	// Notify TUI of connection
	if m.web.program != nil {
		m.web.program.Send(webConnectedMsg{})
	}

	defer func() {
		m.web.mutex.Lock()
		if m.web.conn == conn {
			m.web.conn = nil
		}
		m.web.mutex.Unlock()
		conn.Close(websocket.StatusNormalClosure, "connection closed")

		// Notify TUI of disconnection
		if m.web.program != nil {
			m.web.program.Send(webDisconnectedMsg{})
		}
	}()

	// Note: Snapshot is sent from Update() when webConnectedMsg is received
	// This ensures we access the canonical Model with actual messages

	// Ping keepalive: send ping every 30s to prevent NAT/firewall idle timeout
	ctx, cancelPing := context.WithCancel(context.Background())
	defer cancelPing()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Ping(pingCtx)
				cancel()
				if err != nil {
					conn.Close(websocket.StatusGoingAway, "ping timeout")
					return
				}
			}
		}
	}()

	// Read messages from Web client
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}

		var msg WebMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		// Inject message into TUI via program.Send
		if m.web.program != nil {
			switch msg.Type {
			case "input":
				m.web.program.Send(webInputMsg{content: msg.Content})
			case "command":
				m.web.program.Send(webCommandMsg{name: msg.Content})
			}
		}
	}
}

// sendSnapshotToWeb sends the current state to the connected Web client
// This should be called from Update() to access the canonical Model
// System messages are filtered out from the snapshot
func (m *Model) sendSnapshotToWeb() {
	if m.web == nil {
		return
	}

	m.web.mutex.Lock()
	conn := m.web.conn
	m.web.mutex.Unlock()

	if conn == nil {
		return
	}

	// Convert messages, filtering out system messages
	var messages []WebMessageData
	for _, msg := range m.messages {
		if msg.Type != MessageTypeSystem {
			messages = append(messages, m.messageToWebData(msg))
		}
	}

	// Convert todos
	m.todosMutex.RLock()
	todos := make([]WebTodoData, len(m.todos))
	for i, todo := range m.todos {
		todos[i] = WebTodoData{
			ID:      fmt.Sprintf("%d", i),
			Content: todo.Content,
			Status:  todo.Status,
		}
	}
	m.todosMutex.RUnlock()

	snapshot := WebSnapshot{
		Messages:  messages,
		Todos:     todos,
		IsRunning: m.isRunning,
		Status:    m.statusText,
	}

	event := WebEvent{
		Type: "snapshot",
		Data: snapshot,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Write(ctx, websocket.MessageText, data)
}

// messageToWebData converts a Message to WebMessageData
func (m *Model) messageToWebData(msg Message) WebMessageData {
	var msgType string
	switch msg.Type {
	case MessageTypeUser:
		msgType = "user"
	case MessageTypeAssistant:
		msgType = "assistant"
	case MessageTypeThinking:
		msgType = "thinking"
	case MessageTypeToolCall:
		msgType = "tool_call"
	case MessageTypeToolResult:
		msgType = "tool_result"
	case MessageTypeSystem:
		msgType = "system"
	}

	return WebMessageData{
		Type:      msgType,
		Content:   msg.Content,
		ToolName:  msg.ToolName,
		ToolArgs:  msg.ToolArgs,
		Success:   msg.Success,
		Timestamp: msg.Timestamp.Format(time.RFC3339),
	}
}

// pushToWeb sends an event to the connected Web client
func (m *Model) pushToWeb(eventType string, data interface{}) {
	if m.web == nil {
		return
	}

	m.web.mutex.Lock()
	conn := m.web.conn
	m.web.mutex.Unlock()

	if conn == nil {
		return
	}

	event := WebEvent{
		Type: eventType,
		Data: data,
	}

	jsonData, err := json.Marshal(event)
	if err != nil {
		return
	}

	// Non-blocking send with timeout
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.Write(ctx, websocket.MessageText, jsonData)
	}()
}

// pushMessageToWeb pushes a message event to Web client
func (m *Model) pushMessageToWeb(msg Message) {
	m.pushToWeb("message", m.messageToWebData(msg))
}

// pushMessageUpdateToWeb pushes a message update event to Web client (for streaming)
func (m *Model) pushMessageUpdateToWeb(content string) {
	m.pushToWeb("message_update", map[string]interface{}{
		"content": content,
	})
}

// pushStatusToWeb pushes status update to Web client
func (m *Model) pushStatusToWeb(isRunning bool, status string) {
	m.pushToWeb("status", map[string]interface{}{
		"isRunning": isRunning,
		"status":    status,
	})
}

// pushTodosToWeb pushes todo list update to Web client
func (m *Model) pushTodosToWeb() {
	m.todosMutex.RLock()
	todos := make([]WebTodoData, len(m.todos))
	for i, todo := range m.todos {
		todos[i] = WebTodoData{
			ID:      fmt.Sprintf("%d", i),
			Content: todo.Content,
			Status:  todo.Status,
		}
	}
	m.todosMutex.RUnlock()

	m.pushToWeb("todos", todos)
}

// pushClearToWeb pushes clear event to Web client
func (m *Model) pushClearToWeb() {
	m.pushToWeb("clear", nil)
}

// handleAPIFileList handles GET /api/files - returns directory listing
func (m *Model) handleAPIFileList(w http.ResponseWriter, r *http.Request) {
	// Validate secret
	if !m.validateSecret(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	workspace := m.config.Agent.WorkspaceDir
	reqPath := r.URL.Query().Get("path")

	// Default to workspace root
	if reqPath == "" {
		reqPath = "."
	}

	// Resolve to absolute path
	var absPath string
	if filepath.IsAbs(reqPath) {
		absPath = reqPath
	} else {
		absPath = filepath.Join(workspace, reqPath)
	}

	// Security check: ensure path is within workspace
	absPath = filepath.Clean(absPath)
	workspaceAbs, _ := filepath.Abs(workspace)
	if !strings.HasPrefix(absPath, workspaceAbs) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Read directory
	entries, err := os.ReadDir(absPath)
	if err != nil {
		http.Error(w, "Directory not found", http.StatusNotFound)
		return
	}

	// Build file list (skip dot-prefixed entries)
	files := make([]WebFileItem, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, WebFileItem{
			Name:    entry.Name(),
			IsDir:   entry.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339),
		})
	}

	// Calculate relative path from workspace
	relPath, _ := filepath.Rel(workspaceAbs, absPath)
	if relPath == "." {
		relPath = ""
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WebFileListData{
		Path:  relPath,
		Files: files,
	})
}

// handleAPIFileContent handles GET /api/files/content - returns file content
func (m *Model) handleAPIFileContent(w http.ResponseWriter, r *http.Request) {
	// Validate secret
	if !m.validateSecret(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	workspace := m.config.Agent.WorkspaceDir
	reqPath := r.URL.Query().Get("path")

	if reqPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	// Resolve to absolute path
	var absPath string
	if filepath.IsAbs(reqPath) {
		absPath = reqPath
	} else {
		absPath = filepath.Join(workspace, reqPath)
	}

	// Security check: ensure path is within workspace
	absPath = filepath.Clean(absPath)
	workspaceAbs, _ := filepath.Abs(workspace)
	if !strings.HasPrefix(absPath, workspaceAbs) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Check if file exists
	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "Path is a directory", http.StatusBadRequest)
		return
	}

	// Open file
	file, err := os.Open(absPath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Set appropriate content type
	ext := strings.ToLower(filepath.Ext(absPath))
	contentType := getContentType(ext)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", filepath.Base(absPath)))

	// Stream file directly to response (efficient for large files)
	io.Copy(w, file)
}

// handleAPIFileUpload handles POST /api/files/upload - uploads a file
func (m *Model) handleAPIFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate secret
	if !m.validateSecret(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	workspace := m.config.Agent.WorkspaceDir
	dirPath := r.URL.Query().Get("path")

	// Default to workspace root
	if dirPath == "" {
		dirPath = "."
	}

	// Resolve directory to absolute path
	var absDir string
	if filepath.IsAbs(dirPath) {
		absDir = dirPath
	} else {
		absDir = filepath.Join(workspace, dirPath)
	}

	// Security check: ensure path is within workspace
	absDir = filepath.Clean(absDir)
	workspaceAbs, _ := filepath.Abs(workspace)
	if !strings.HasPrefix(absDir, workspaceAbs) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Parse multipart form (32MB max)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Ensure directory exists
	if err := os.MkdirAll(absDir, 0755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	// Create destination file
	filePath := filepath.Join(absDir, header.Filename)
	dst, err := os.Create(filePath)
	if err != nil {
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Copy file content
	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}

	// Calculate relative path
	relPath, _ := filepath.Rel(workspaceAbs, filePath)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WebFileUploadResult{
		Success: true,
		Path:    relPath,
	})
}

// handleAPIFileDelete handles POST /api/files/delete - deletes a file or directory
func (m *Model) handleAPIFileDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate secret
	if !m.validateSecret(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	workspace := m.config.Agent.WorkspaceDir
	reqPath := r.URL.Query().Get("path")

	if reqPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	// Resolve to absolute path
	var absPath string
	if filepath.IsAbs(reqPath) {
		absPath = reqPath
	} else {
		absPath = filepath.Join(workspace, reqPath)
	}

	// Security check: ensure path is within workspace
	absPath = filepath.Clean(absPath)
	workspaceAbs, _ := filepath.Abs(workspace)
	if !strings.HasPrefix(absPath, workspaceAbs) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Prevent deleting the workspace root itself
	if absPath == workspaceAbs {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(WebFileDeleteResult{
			Success: false,
			Error:   "Cannot delete workspace root",
		})
		return
	}

	// Check if path exists
	if _, err := os.Stat(absPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(WebFileDeleteResult{
			Success: false,
			Error:   "File not found",
		})
		return
	}

	// Delete file or directory
	if err := os.RemoveAll(absPath); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(WebFileDeleteResult{
			Success: false,
			Error:   "Failed to delete: " + err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(WebFileDeleteResult{
		Success: true,
	})
}

// getContentType returns the MIME type for a file extension
func getContentType(ext string) string {
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".txt", ".md", ".go", ".py", ".rs", ".ts", ".tsx", ".jsx":
		return "text/plain; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".zip":
		return "application/zip"
	case ".tar":
		return "application/x-tar"
	case ".gz":
		return "application/gzip"
	default:
		return "application/octet-stream"
	}
}

// markedJS is the embedded marked.js library for Markdown rendering (v15.0.12)
// https://github.com/markedjs/marked - MIT Licensed
const markedJS = `(function(g,f){if(typeof exports=="object"&&typeof module<"u"){module.exports=f()}else if("function"==typeof define && define.amd){define("marked",f)}else {g["marked"]=f()}}(typeof globalThis < "u" ? globalThis : typeof self < "u" ? self : this,function(){var exports={};var __exports=exports;var module={exports};
"use strict";var H=Object.defineProperty;var be=Object.getOwnPropertyDescriptor;var Te=Object.getOwnPropertyNames;var we=Object.prototype.hasOwnProperty;var ye=(l,e)=>{for(var t in e)H(l,t,{get:e[t],enumerable:!0})},Re=(l,e,t,n)=>{if(e&&typeof e=="object"||typeof e=="function")for(let s of Te(e))!we.call(l,s)&&s!==t&&H(l,s,{get:()=>e[s],enumerable:!(n=be(e,s))||n.enumerable});return l};var Se=l=>Re(H({},"__esModule",{value:!0}),l);var kt={};ye(kt,{Hooks:()=>L,Lexer:()=>x,Marked:()=>E,Parser:()=>b,Renderer:()=>$,TextRenderer:()=>_,Tokenizer:()=>S,defaults:()=>w,getDefaults:()=>z,lexer:()=>ht,marked:()=>k,options:()=>it,parse:()=>pt,parseInline:()=>ct,parser:()=>ut,setOptions:()=>ot,use:()=>lt,walkTokens:()=>at});module.exports=Se(kt);function z(){return{async:!1,breaks:!1,extensions:null,gfm:!0,hooks:null,pedantic:!1,renderer:null,silent:!1,tokenizer:null,walkTokens:null}}var w=z();function N(l){w=l}var I={exec:()=>null};function h(l,e=""){let t=typeof l=="string"?l:l.source,n={replace:(s,i)=>{let r=typeof i=="string"?i:i.source;return r=r.replace(m.caret,"$1"),t=t.replace(s,r),n},getRegex:()=>new RegExp(t,e)};return n}var m={codeRemoveIndent:/^(?: {1,4}| {0,3}\t)/gm,outputLinkReplace:/\\([\[\]])/g,indentCodeCompensation:/^(\s+)(?:` + "`" + `{3})/,beginningSpace:/^\s+/,endingHash:/#$/,startingSpaceChar:/^ /,endingSpaceChar:/ $/,nonSpaceChar:/[^ ]/,newLineCharGlobal:/\n/g,tabCharGlobal:/\t/g,multipleSpaceGlobal:/\s+/g,blankLine:/^[ \t]*$/,doubleBlankLine:/\n[ \t]*\n[ \t]*$/,blockquoteStart:/^ {0,3}>/,blockquoteSetextReplace:/\n {0,3}((?:=+|-+) *)(?=\n|$)/g,blockquoteSetextReplace2:/^ {0,3}>[ \t]?/gm,listReplaceTabs:/^\t+/,listReplaceNesting:/^ {1,4}(?=( {4})*[^ ])/g,listIsTask:/^\[[ xX]\] /,listReplaceTask:/^\[[ xX]\] +/,anyLine:/\n.*\n/,hrefBrackets:/^<(.*)>$/,tableDelimiter:/[:|]/,tableAlignChars:/^\||\| *$/g,tableRowBlankLine:/\n[ \t]*$/,tableAlignRight:/^ *-+: *$/,tableAlignCenter:/^ *:-+: *$/,tableAlignLeft:/^ *:-+ *$/,startATag:/^<a /i,endATag:/^<\/a>/i,startPreScriptTag:/^<(pre|code|kbd|script)(\s|>)/i,endPreScriptTag:/^<\/(pre|code|kbd|script)(\s|>)/i,startAngleBracket:/^</,endAngleBracket:/>$/,pedanticHrefTitle:/^([^'"]*[^\s])\s+(['"])(.*)\2/,unicodeAlphaNumeric:/[\p{L}\p{N}]/u,escapeTest:/[&<>"']/,escapeReplace:/[&<>"']/g,escapeTestNoEncode:/[<>"']|&(?!(#\d{1,7}|#[Xx][a-fA-F0-9]{1,6}|\w+);)/,escapeReplaceNoEncode:/[<>"']|&(?!(#\d{1,7}|#[Xx][a-fA-F0-9]{1,6}|\w+);)/g,unescapeTest:/&(#(?:\d+)|(?:#x[0-9A-Fa-f]+)|(?:\w+));?/ig,caret:/(^|[^\[])\^/g,percentDecode:/%25/g,findPipe:/\|/g,splitPipe:/ \|/,slashPipe:/\\\|/g,carriageReturn:/\r\n|\r/g,spaceLine:/^ +$/gm,notSpaceStart:/^\S*/,endingNewline:/\n$/,listItemRegex:l=>new RegExp(` + "`" + `^( {0,3}${l})((?:[	 ][^\\n]*)?(?:\\n|$))` + "`" + `),nextBulletRegex:l=>new RegExp(` + "`" + `^ {0,${Math.min(3,l-1)}}(?:[*+-]|\\d{1,9}[.)])((?:[ 	][^\\n]*)?(?:\\n|$))` + "`" + `),hrRegex:l=>new RegExp(` + "`" + `^ {0,${Math.min(3,l-1)}}((?:- *){3,}|(?:_ *){3,}|(?:\\* *){3,})(?:\\n+|$)` + "`" + `),fencesBeginRegex:l=>new RegExp(` + "`" + `^ {0,${Math.min(3,l-1)}}(?:\` + "`" + `\` + "`" + `\` + "`" + `|~~~)` + "`" + `),headingBeginRegex:l=>new RegExp(` + "`" + `^ {0,${Math.min(3,l-1)}}#` + "`" + `),htmlBeginRegex:l=>new RegExp(` + "`" + `^ {0,${Math.min(3,l-1)}}<(?:[a-z].*>|!--)` + "`" + `,"i")},$e=/^(?:[ \t]*(?:\n|$))+/,_e=/^((?: {4}| {0,3}\t)[^\n]+(?:\n(?:[ \t]*(?:\n|$))*)?)+/,Le=/^ {0,3}(` + "`" + `{3,}(?=[^` + "`" + `\n]*(?:\n|$))|~{3,})([^\n]*)(?:\n|$)(?:|([\s\S]*?)(?:\n|$))(?: {0,3}\1[~` + "`" + `]* *(?=\n|$)|$)/,O=/^ {0,3}((?:-[\t ]*){3,}|(?:_[ \t]*){3,}|(?:\*[ \t]*){3,})(?:\n+|$)/,ze=/^ {0,3}(#{1,6})(?=\s|$)(.*)(?:\n+|$)/,F=/(?:[*+-]|\d{1,9}[.)])/,ie=/^(?!bull |blockCode|fences|blockquote|heading|html|table)((?:.|\n(?!\s*?\n|bull |blockCode|fences|blockquote|heading|html|table))+?)\n {0,3}(=+|-+) *(?:\n+|$)/,oe=h(ie).replace(/bull/g,F).replace(/blockCode/g,/(?: {4}| {0,3}\t)/).replace(/fences/g,/ {0,3}(?:` + "`" + `{3,}|~{3,})/).replace(/blockquote/g,/ {0,3}>/).replace(/heading/g,/ {0,3}#{1,6}/).replace(/html/g,/ {0,3}<[^\n>]+>\n/).replace(/\|table/g,"").getRegex(),Me=h(ie).replace(/bull/g,F).replace(/blockCode/g,/(?: {4}| {0,3}\t)/).replace(/fences/g,/ {0,3}(?:` + "`" + `{3,}|~{3,})/).replace(/blockquote/g,/ {0,3}>/).replace(/heading/g,/ {0,3}#{1,6}/).replace(/html/g,/ {0,3}<[^\n>]+>\n/).replace(/table/g,/ {0,3}\|?(?:[:\- ]*\|)+[\:\- ]*\n/).getRegex(),Q=/^([^\n]+(?:\n(?!hr|heading|lheading|blockquote|fences|list|html|table| +\n)[^\n]+)*)/,Pe=/^[^\n]+/,U=/(?!\s*\])(?:\\.|[^\[\]\\])+/,Ae=h(/^ {0,3}\[(label)\]: *(?:\n[ \t]*)?([^<\s][^\s]*|<.*?>)(?:(?: +(?:\n[ \t]*)?| *\n[ \t]*)(title))? *(?:\n+|$)/).replace("label",U).replace("title",/(?:"(?:\\"?|[^"\\])*"|'[^'\n]*(?:\n[^'\n]+)*\n?'|\([^()]*\))/).getRegex(),Ee=h(/^( {0,3}bull)([ \t][^\n]+?)?(?:\n|$)/).replace(/bull/g,F).getRegex(),v="address|article|aside|base|basefont|blockquote|body|caption|center|col|colgroup|dd|details|dialog|dir|div|dl|dt|fieldset|figcaption|figure|footer|form|frame|frameset|h[1-6]|head|header|hr|html|iframe|legend|li|link|main|menu|menuitem|meta|nav|noframes|ol|optgroup|option|p|param|search|section|summary|table|tbody|td|tfoot|th|thead|title|tr|track|ul",K=/<!--(?:-?>|[\s\S]*?(?:-->|$))/,Ce=h("^ {0,3}(?:<(script|pre|style|textarea)[\\s>][\\s\\S]*?(?:</\\1>[^\\n]*\\n+|$)|comment[^\\n]*(\\n+|$)|<\\?[\\s\\S]*?(?:\\?>\\n*|$)|<![A-Z][\\s\\S]*?(?:>\\n*|$)|<!\\[CDATA\\[[\\s\\S]*?(?:\\]\\]>\\n*|$)|</?(tag)(?: +|\\n|/?>)[\\s\\S]*?(?:(?:\\n[ 	]*)+\\n|$)|<(?!script|pre|style|textarea)([a-z][\\w-]*)(?:attribute)*? */?>(?=[ \\t]*(?:\\n|$))[\\s\\S]*?(?:(?:\\n[ 	]*)+\\n|$)|</(?!script|pre|style|textarea)[a-z][\\w-]*\\s*>(?=[ \\t]*(?:\\n|$))[\\s\\S]*?(?:(?:\\n[ 	]*)+\\n|$))","i").replace("comment",K).replace("tag",v).replace("attribute",/ +[a-zA-Z:_][\w.:-]*(?: *= *"[^"\n]*"| *= *'[^'\n]*'| *= *[^\s"'=<>` + "`" + `]+)?/).getRegex(),le=h(Q).replace("hr",O).replace("heading"," {0,3}#{1,6}(?:\\s|$)").replace("|lheading","").replace("|table","").replace("blockquote"," {0,3}>").replace("fences"," {0,3}(?:` + "`" + `{3,}(?=[^` + "`" + `\\n]*\\n)|~{3,})[^\\n]*\\n").replace("list"," {0,3}(?:[*+-]|1[.)]) ").replace("html","</?(?:tag)(?: +|\\n|/?>)|<(?:script|pre|style|textarea|!--)").replace("tag",v).getRegex(),Ie=h(/^( {0,3}> ?(paragraph|[^\n]*)(?:\n|$))+/).replace("paragraph",le).getRegex(),X={blockquote:Ie,code:_e,def:Ae,fences:Le,heading:ze,hr:O,html:Ce,lheading:oe,list:Ee,newline:$e,paragraph:le,table:I,text:Pe},re=h("^ *([^\\n ].*)\\n {0,3}((?:\\| *)?:?-+:? *(?:\\| *:?-+:? *)*(?:\\| *)?)(?:\\n((?:(?! *\\n|hr|heading|blockquote|code|fences|list|html).*(?:\\n|$))*)\\n*|$)").replace("hr",O).replace("heading"," {0,3}#{1,6}(?:\\s|$)").replace("blockquote"," {0,3}>").replace("code","(?: {4}| {0,3}	)[^\\n]").replace("fences"," {0,3}(?:` + "`" + `{3,}(?=[^` + "`" + `\\n]*\\n)|~{3,})[^\\n]*\\n").replace("list"," {0,3}(?:[*+-]|1[.)]) ").replace("html","</?(?:tag)(?: +|\\n|/?>)|<(?:script|pre|style|textarea|!--)").replace("tag",v).getRegex(),Oe={...X,lheading:Me,table:re,paragraph:h(Q).replace("hr",O).replace("heading"," {0,3}#{1,6}(?:\\s|$)").replace("|lheading","").replace("table",re).replace("blockquote"," {0,3}>").replace("fences"," {0,3}(?:` + "`" + `{3,}(?=[^` + "`" + `\\n]*\\n)|~{3,})[^\\n]*\\n").replace("list"," {0,3}(?:[*+-]|1[.)]) ").replace("html","</?(?:tag)(?: +|\\n|/?>)|<(?:script|pre|style|textarea|!--)").replace("tag",v).getRegex()},Be={...X,html:h(` + "`" + `^ *(?:comment *(?:\\n|\\s*$)|<(tag)[\\s\\S]+?</\\1> *(?:\\n{2,}|\\s*$)|<tag(?:"[^"]*"|'[^']*'|\\s[^'"/>\\s]*)*?/?> *(?:\\n{2,}|\\s*$))` + "`" + `).replace("comment",K).replace(/tag/g,"(?!(?:a|em|strong|small|s|cite|q|dfn|abbr|data|time|code|var|samp|kbd|sub|sup|i|b|u|mark|ruby|rt|rp|bdi|bdo|span|br|wbr|ins|del|img)\\b)\\w+(?!:|[^\\w\\s@]*@)\\b").getRegex(),def:/^ *\[([^\]]+)\]: *<?([^\s>]+)>?(?: +(["(][^\n]+[")]))? *(?:\n+|$)/,heading:/^(#{1,6})(.*)(?:\n+|$)/,fences:I,lheading:/^(.+?)\n {0,3}(=+|-+) *(?:\n+|$)/,paragraph:h(Q).replace("hr",O).replace("heading",` + "`" + ` *#{1,6} *[^\n]` + "`" + `).replace("lheading",oe).replace("|table","").replace("blockquote"," {0,3}>").replace("|fences","").replace("|list","").replace("|html","").replace("|tag","").getRegex()},qe=/^\\([!"#$%&'()*+,\-./:;<=>?@\[\]\\^_` + "`" + `{|}~])/,ve=/^(` + "`" + `+)([^` + "`" + `]|[^` + "`" + `][\s\S]*?[^` + "`" + `])\1(?!` + "`" + `)/,ae=/^( {2,}|\\)\n(?!\s*$)/,De=/^(` + "`" + `+|[^` + "`" + `])(?:(?= {2,}\n)|[\s\S]*?(?:(?=[\\<!\[` + "`" + `*_]|\b_|$)|[^ ](?= {2,}\n)))/,D=/[\p{P}\p{S}]/u,W=/[\s\p{P}\p{S}]/u,ce=/[^\s\p{P}\p{S}]/u,Ze=h(/^((?![*_])punctSpace)/,"u").replace(/punctSpace/g,W).getRegex(),pe=/(?!~)[\p{P}\p{S}]/u,Ge=/(?!~)[\s\p{P}\p{S}]/u,He=/(?:[^\s\p{P}\p{S}]|~)/u,Ne=/\[[^[\]]*?\]\((?:\\.|[^\\\(\)]|\((?:\\.|[^\\\(\)])*\))*\)|` + "`" + `[^` + "`" + `]*?` + "`" + `|<[^<>]*?>/g,ue=/^(?:\*+(?:((?!\*)punct)|[^\s*]))|^_+(?:((?!_)punct)|([^\s_]))/,je=h(ue,"u").replace(/punct/g,D).getRegex(),Fe=h(ue,"u").replace(/punct/g,pe).getRegex(),he="^[^_*]*?__[^_*]*?\\*[^_*]*?(?=__)|[^*]+(?=[^*])|(?!\\*)punct(\\*+)(?=[\\s]|$)|notPunctSpace(\\*+)(?!\\*)(?=punctSpace|$)|(?!\\*)punctSpace(\\*+)(?=notPunctSpace)|[\\s](\\*+)(?!\\*)(?=punct)|(?!\\*)punct(\\*+)(?!\\*)(?=punct)|notPunctSpace(\\*+)(?=notPunctSpace)",Qe=h(he,"gu").replace(/notPunctSpace/g,ce).replace(/punctSpace/g,W).replace(/punct/g,D).getRegex(),Ue=h(he,"gu").replace(/notPunctSpace/g,He).replace(/punctSpace/g,Ge).replace(/punct/g,pe).getRegex(),Ke=h("^[^_*]*?\\*\\*[^_*]*?_[^_*]*?(?=\\*\\*)|[^_]+(?=[^_])|(?!_)punct(_+)(?=[\\s]|$)|notPunctSpace(_+)(?!_)(?=punctSpace|$)|(?!_)punctSpace(_+)(?=notPunctSpace)|[\\s](_+)(?!_)(?=punct)|(?!_)punct(_+)(?!_)(?=punct)","gu").replace(/notPunctSpace/g,ce).replace(/punctSpace/g,W).replace(/punct/g,D).getRegex(),Xe=h(/\\(punct)/,"gu").replace(/punct/g,D).getRegex(),We=h(/^<(scheme:[^\s\x00-\x1f<>]*|email)>/).replace("scheme",/[a-zA-Z][a-zA-Z0-9+.-]{1,31}/).replace("email",/[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+(@)[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+(?![-_])/).getRegex(),Je=h(K).replace("(?:-->|$)","-->").getRegex(),Ve=h("^comment|^</[a-zA-Z][\\w:-]*\\s*>|^<[a-zA-Z][\\w-]*(?:attribute)*?\\s*/?>|^<\\?[\\s\\S]*?\\?>|^<![a-zA-Z]+\\s[\\s\\S]*?>|^<!\\[CDATA\\[[\\s\\S]*?\\]\\]>").replace("comment",Je).replace("attribute",/\s+[a-zA-Z:_][\w.:-]*(?:\s*=\s*"[^"]*"|\s*=\s*'[^']*'|\s*=\s*[^\s"'=<>` + "`" + `]+)?/).getRegex(),q=/(?:\[(?:\\.|[^\[\]\\])*\]|\\.|` + "`" + `[^` + "`" + `]*` + "`" + `|[^\[\]\\` + "`" + `])*?/,Ye=h(/^!?\[(label)\]\(\s*(href)(?:(?:[ \t]*(?:\n[ \t]*)?)(title))?\s*\)/).replace("label",q).replace("href",/<(?:\\.|[^\n<>\\])+>|[^ \t\n\x00-\x1f]*/).replace("title",/"(?:\\"?|[^"\\])*"|'(?:\\'?|[^'\\])*'|\((?:\\\)?|[^)\\])*\)/).getRegex(),ke=h(/^!?\[(label)\]\[(ref)\]/).replace("label",q).replace("ref",U).getRegex(),ge=h(/^!?\[(ref)\](?:\[\])?/).replace("ref",U).getRegex(),et=h("reflink|nolink(?!\\()","g").replace("reflink",ke).replace("nolink",ge).getRegex(),J={_backpedal:I,anyPunctuation:Xe,autolink:We,blockSkip:Ne,br:ae,code:ve,del:I,emStrongLDelim:je,emStrongRDelimAst:Qe,emStrongRDelimUnd:Ke,escape:qe,link:Ye,nolink:ge,punctuation:Ze,reflink:ke,reflinkSearch:et,tag:Ve,text:De,url:I},tt={...J,link:h(/^!?\[(label)\]\((.*?)\)/).replace("label",q).getRegex(),reflink:h(/^!?\[(label)\]\s*\[([^\]]*)\]/).replace("label",q).getRegex()},j={...J,emStrongRDelimAst:Ue,emStrongLDelim:Fe,url:h(/^((?:ftp|https?):\/\/|www\.)(?:[a-zA-Z0-9\-]+\.?)+[^\s<]*|^email/,"i").replace("email",/[A-Za-z0-9._+-]+(@)[a-zA-Z0-9-_]+(?:\.[a-zA-Z0-9-_]*[a-zA-Z0-9])+(?![-_])/).getRegex(),_backpedal:/(?:[^?!.,:;*_'"~()&]+|\([^)]*\)|&(?![a-zA-Z0-9]+;$)|[?!.,:;*_'"~)]+(?!$))+/,del:/^(~~?)(?=[^\s~])((?:\\.|[^\\])*?(?:\\.|[^\s~\\]))\1(?=[^~]|$)/,text:/^([` + "`" + `~]+|[^` + "`" + `~])(?:(?= {2,}\n)|(?=[a-zA-Z0-9.!#$%&'*+\/=?_` + "`" + `{\|}~-]+@)|[\s\S]*?(?:(?=[\\<!\[` + "`" + `*~_]|\b_|https?:\/\/|ftp:\/\/|www\.|$)|[^ ](?= {2,}\n)|[^a-zA-Z0-9.!#$%&'*+\/=?_` + "`" + `{\|}~-](?=[a-zA-Z0-9.!#$%&'*+\/=?_` + "`" + `{\|}~-]+@)))/},nt={...j,br:h(ae).replace("{2,}","*").getRegex(),text:h(j.text).replace("\\b_","\\b_| {2,}\\n").replace(/\{2,\}/g,"*").getRegex()},B={normal:X,gfm:Oe,pedantic:Be},P={normal:J,gfm:j,breaks:nt,pedantic:tt};var st={"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"},fe=l=>st[l];function R(l,e){if(e){if(m.escapeTest.test(l))return l.replace(m.escapeReplace,fe)}else if(m.escapeTestNoEncode.test(l))return l.replace(m.escapeReplaceNoEncode,fe);return l}function V(l){try{l=encodeURI(l).replace(m.percentDecode,"%")}catch{return null}return l}function Y(l,e){let t=l.replace(m.findPipe,(i,r,o)=>{let a=!1,c=r;for(;--c>=0&&o[c]==="\\\\";)a=!a;return a?"|":" |"}),n=t.split(m.splitPipe),s=0;if(n[0].trim()||n.shift(),n.length>0&&!n.at(-1)?.trim()&&n.pop(),e)if(n.length>e)n.splice(e);else for(;n.length<e;)n.push("");for(;s<n.length;s++)n[s]=n[s].trim().replace(m.slashPipe,"|");return n}function A(l,e,t){let n=l.length;if(n===0)return"";let s=0;for(;s<n;){let i=l.charAt(n-s-1);if(i===e&&!t)s++;else if(i!==e&&t)s++;else break}return l.slice(0,n-s)}function de(l,e){if(l.indexOf(e[1])===-1)return-1;let t=0;for(let n=0;n<l.length;n++)if(l[n]==="\\\\")n++;else if(l[n]===e[0])t++;else if(l[n]===e[1]&&(t--,t<0))return n;return t>0?-2:-1}function me(l,e,t,n,s){let i=e.href,r=e.title||null,o=l[1].replace(s.other.outputLinkReplace,"$1");n.state.inLink=!0;let a={type:l[0].charAt(0)==="!"?"image":"link",raw:t,href:i,title:r,text:o,tokens:n.inlineTokens(o)};return n.state.inLink=!1,a}function rt(l,e,t){let n=l.match(t.other.indentCodeCompensation);if(n===null)return e;let s=n[1];return e.split("\n").map(i=>{let r=i.match(t.other.beginningSpace);if(r===null)return i;let[o]=r;return o.length>=s.length?i.slice(s.length):i}).join("\n")}var S=class{options;rules;lexer;constructor(e){this.options=e||w}space(e){let t=this.rules.block.newline.exec(e);if(t&&t[0].length>0)return{type:"space",raw:t[0]}}code(e){let t=this.rules.block.code.exec(e);if(t){let n=t[0].replace(this.rules.other.codeRemoveIndent,"");return{type:"code",raw:t[0],codeBlockStyle:"indented",text:this.options.pedantic?n:A(n,"\n")}}}fences(e){let t=this.rules.block.fences.exec(e);if(t){let n=t[0],s=rt(n,t[3]||"",this.rules);return{type:"code",raw:n,lang:t[2]?t[2].trim().replace(this.rules.inline.anyPunctuation,"$1"):t[2],text:s}}}heading(e){let t=this.rules.block.heading.exec(e);if(t){let n=t[2].trim();if(this.rules.other.endingHash.test(n)){let s=A(n,"#");(this.options.pedantic||!s||this.rules.other.endingSpaceChar.test(s))&&(n=s.trim())}return{type:"heading",raw:t[0],depth:t[1].length,text:n,tokens:this.lexer.inline(n)}}}hr(e){let t=this.rules.block.hr.exec(e);if(t)return{type:"hr",raw:A(t[0],"\n")}}blockquote(e){let t=this.rules.block.blockquote.exec(e);if(t){let n=A(t[0],"\n").split("\n"),s="",i="",r=[];for(;n.length>0;){let o=!1,a=[],c;for(c=0;c<n.length;c++)if(this.rules.other.blockquoteStart.test(n[c]))a.push(n[c]),o=!0;else if(!o)a.push(n[c]);else break;n=n.slice(c);let p=a.join("\n"),u=p.replace(this.rules.other.blockquoteSetextReplace,"\n    $1").replace(this.rules.other.blockquoteSetextReplace2,"");s=s?s+"\n"+p:p,i=i?i+"\n"+u:u;let d=this.lexer.state.top;if(this.lexer.state.top=!0,this.lexer.blockTokens(u,r,!0),this.lexer.state.top=d,n.length===0)break;let g=r.at(-1);if(g?.type==="code")break;if(g?.type==="blockquote"){let T=g,f=T.raw+"\n"+n.join("\n"),y=this.blockquote(f);r[r.length-1]=y,s=s.substring(0,s.length-T.raw.length)+y.raw,i=i.substring(0,i.length-T.text.length)+y.text;break}else if(g?.type==="list"){let T=g,f=T.raw+"\n"+n.join("\n"),y=this.list(f);r[r.length-1]=y,s=s.substring(0,s.length-g.raw.length)+y.raw,i=i.substring(0,i.length-T.raw.length)+y.raw,n=f.substring(r.at(-1).raw.length).split("\n");continue}}return{type:"blockquote",raw:s,tokens:r,text:i}}}list(e){let t=this.rules.block.list.exec(e);if(t){let n=t[1].trim(),s=n.length>1,i={type:"list",raw:"",ordered:s,start:s?+n.slice(0,-1):"",loose:!1,items:[]};n=s?"\\\\d{1,9}\\\\"+n.slice(-1):"\\\\"+n,this.options.pedantic&&(n=s?n:"[*+-]");let r=this.rules.other.listItemRegex(n),o=!1;for(;e;){let c=!1,p="",u="";if(!(t=r.exec(e))||this.rules.block.hr.test(e))break;p=t[0],e=e.substring(p.length);let d=t[2].split("\n",1)[0].replace(this.rules.other.listReplaceTabs,Z=>" ".repeat(3*Z.length)),g=e.split("\n",1)[0],T=!d.trim(),f=0;if(this.options.pedantic?(f=2,u=d.trimStart()):T?f=t[1].length+1:(f=t[2].search(this.rules.other.nonSpaceChar),f=f>4?1:f,u=d.slice(f),f+=t[1].length),T&&this.rules.other.blankLine.test(g)&&(p+=g+"\n",e=e.substring(g.length+1),c=!0),!c){let Z=this.rules.other.nextBulletRegex(f),te=this.rules.other.hrRegex(f),ne=this.rules.other.fencesBeginRegex(f),se=this.rules.other.headingBeginRegex(f),xe=this.rules.other.htmlBeginRegex(f);for(;e;){let G=e.split("\n",1)[0],C;if(g=G,this.options.pedantic?(g=g.replace(this.rules.other.listReplaceNesting,"  "),C=g):C=g.replace(this.rules.other.tabCharGlobal,"    "),ne.test(g)||se.test(g)||xe.test(g)||Z.test(g)||te.test(g))break;if(C.search(this.rules.other.nonSpaceChar)>=f||!g.trim())u+="\n"+C.slice(f);else{if(T||d.replace(this.rules.other.tabCharGlobal,"    ").search(this.rules.other.nonSpaceChar)>=4||ne.test(d)||se.test(d)||te.test(d))break;u+="\n"+g}!T&&!g.trim()&&(T=!0),p+=G+"\n",e=e.substring(G.length+1),d=C.slice(f)}}i.loose||(o?i.loose=!0:this.rules.other.doubleBlankLine.test(p)&&(o=!0));let y=null,ee;this.options.gfm&&(y=this.rules.other.listIsTask.exec(u),y&&(ee=y[0]!=="[ ] ",u=u.replace(this.rules.other.listReplaceTask,""))),i.items.push({type:"list_item",raw:p,task:!!y,checked:ee,loose:!1,text:u,tokens:[]}),i.raw+=p}let a=i.items.at(-1);if(a)a.raw=a.raw.trimEnd(),a.text=a.text.trimEnd();else return;i.raw=i.raw.trimEnd();for(let c=0;c<i.items.length;c++)if(this.lexer.state.top=!1,i.items[c].tokens=this.lexer.blockTokens(i.items[c].text,[]),!i.loose){let p=i.items[c].tokens.filter(d=>d.type==="space"),u=p.length>0&&p.some(d=>this.rules.other.anyLine.test(d.raw));i.loose=u}if(i.loose)for(let c=0;c<i.items.length;c++)i.items[c].loose=!0;return i}}html(e){let t=this.rules.block.html.exec(e);if(t)return{type:"html",block:!0,raw:t[0],pre:t[1]==="pre"||t[1]==="script"||t[1]==="style",text:t[0]}}def(e){let t=this.rules.block.def.exec(e);if(t){let n=t[1].toLowerCase().replace(this.rules.other.multipleSpaceGlobal," "),s=t[2]?t[2].replace(this.rules.other.hrefBrackets,"$1").replace(this.rules.inline.anyPunctuation,"$1"):"",i=t[3]?t[3].substring(1,t[3].length-1).replace(this.rules.inline.anyPunctuation,"$1"):t[3];return{type:"def",tag:n,raw:t[0],href:s,title:i}}}table(e){let t=this.rules.block.table.exec(e);if(!t||!this.rules.other.tableDelimiter.test(t[2]))return;let n=Y(t[1]),s=t[2].replace(this.rules.other.tableAlignChars,"").split("|"),i=t[3]?.trim()?t[3].replace(this.rules.other.tableRowBlankLine,"").split("\n"):[],r={type:"table",raw:t[0],header:[],align:[],rows:[]};if(n.length===s.length){for(let o of s)this.rules.other.tableAlignRight.test(o)?r.align.push("right"):this.rules.other.tableAlignCenter.test(o)?r.align.push("center"):this.rules.other.tableAlignLeft.test(o)?r.align.push("left"):r.align.push(null);for(let o=0;o<n.length;o++)r.header.push({text:n[o],tokens:this.lexer.inline(n[o]),header:!0,align:r.align[o]});for(let o of i)r.rows.push(Y(o,r.header.length).map((a,c)=>({text:a,tokens:this.lexer.inline(a),header:!1,align:r.align[c]})));return r}}lheading(e){let t=this.rules.block.lheading.exec(e);if(t)return{type:"heading",raw:t[0],depth:t[2].charAt(0)==="="?1:2,text:t[1],tokens:this.lexer.inline(t[1])}}paragraph(e){let t=this.rules.block.paragraph.exec(e);if(t){let n=t[1].charAt(t[1].length-1)==="\n"?t[1].slice(0,-1):t[1];return{type:"paragraph",raw:t[0],text:n,tokens:this.lexer.inline(n)}}}text(e){let t=this.rules.block.text.exec(e);if(t)return{type:"text",raw:t[0],text:t[0],tokens:this.lexer.inline(t[0])}}escape(e){let t=this.rules.inline.escape.exec(e);if(t)return{type:"escape",raw:t[0],text:t[1]}}tag(e){let t=this.rules.inline.tag.exec(e);if(t)return!this.lexer.state.inLink&&this.rules.other.startATag.test(t[0])?this.lexer.state.inLink=!0:this.lexer.state.inLink&&this.rules.other.endATag.test(t[0])&&(this.lexer.state.inLink=!1),!this.lexer.state.inRawBlock&&this.rules.other.startPreScriptTag.test(t[0])?this.lexer.state.inRawBlock=!0:this.lexer.state.inRawBlock&&this.rules.other.endPreScriptTag.test(t[0])&&(this.lexer.state.inRawBlock=!1),{type:"html",raw:t[0],inLink:this.lexer.state.inLink,inRawBlock:this.lexer.state.inRawBlock,block:!1,text:t[0]}}link(e){let t=this.rules.inline.link.exec(e);if(t){let n=t[2].trim();if(!this.options.pedantic&&this.rules.other.startAngleBracket.test(n)){if(!this.rules.other.endAngleBracket.test(n))return;let r=A(n.slice(0,-1),"\\\\");if((n.length-r.length)%2===0)return}else{let r=de(t[2],"()");if(r===-2)return;if(r>-1){let a=(t[0].indexOf("!")===0?5:4)+t[1].length+r;t[2]=t[2].substring(0,r),t[0]=t[0].substring(0,a).trim(),t[3]=""}}let s=t[2],i="";if(this.options.pedantic){let r=this.rules.other.pedanticHrefTitle.exec(s);r&&(s=r[1],i=r[3])}else i=t[3]?t[3].slice(1,-1):"";return s=s.trim(),this.rules.other.startAngleBracket.test(s)&&(this.options.pedantic&&!this.rules.other.endAngleBracket.test(n)?s=s.slice(1):s=s.slice(1,-1)),me(t,{href:s&&s.replace(this.rules.inline.anyPunctuation,"$1"),title:i&&i.replace(this.rules.inline.anyPunctuation,"$1")},t[0],this.lexer,this.rules)}}reflink(e,t){let n;if((n=this.rules.inline.reflink.exec(e))||(n=this.rules.inline.nolink.exec(e))){let s=(n[2]||n[1]).replace(this.rules.other.multipleSpaceGlobal," "),i=t[s.toLowerCase()];if(!i){let r=n[0].charAt(0);return{type:"text",raw:r,text:r}}return me(n,i,n[0],this.lexer,this.rules)}}emStrong(e,t,n=""){let s=this.rules.inline.emStrongLDelim.exec(e);if(!s||s[3]&&n.match(this.rules.other.unicodeAlphaNumeric))return;if(!(s[1]||s[2]||"")||!n||this.rules.inline.punctuation.exec(n)){let r=[...s[0]].length-1,o,a,c=r,p=0,u=s[0][0]==="*"?this.rules.inline.emStrongRDelimAst:this.rules.inline.emStrongRDelimUnd;for(u.lastIndex=0,t=t.slice(-1*e.length+r);(s=u.exec(t))!=null;){if(o=s[1]||s[2]||s[3]||s[4]||s[5]||s[6],!o)continue;if(a=[...o].length,s[3]||s[4]){c+=a;continue}else if((s[5]||s[6])&&r%3&&!((r+a)%3)){p+=a;continue}if(c-=a,c>0)continue;a=Math.min(a,a+c+p);let d=[...s[0]][0].length,g=e.slice(0,r+s.index+d+a);if(Math.min(r,a)%2){let f=g.slice(1,-1);return{type:"em",raw:g,text:f,tokens:this.lexer.inlineTokens(f)}}let T=g.slice(2,-2);return{type:"strong",raw:g,text:T,tokens:this.lexer.inlineTokens(T)}}}}codespan(e){let t=this.rules.inline.code.exec(e);if(t){let n=t[2].replace(this.rules.other.newLineCharGlobal," "),s=this.rules.other.nonSpaceChar.test(n),i=this.rules.other.startingSpaceChar.test(n)&&this.rules.other.endingSpaceChar.test(n);return s&&i&&(n=n.substring(1,n.length-1)),{type:"codespan",raw:t[0],text:n}}}br(e){let t=this.rules.inline.br.exec(e);if(t)return{type:"br",raw:t[0]}}del(e){let t=this.rules.inline.del.exec(e);if(t)return{type:"del",raw:t[0],text:t[2],tokens:this.lexer.inlineTokens(t[2])}}autolink(e){let t=this.rules.inline.autolink.exec(e);if(t){let n,s;return t[2]==="@"?(n=t[1],s="mailto:"+n):(n=t[1],s=n),{type:"link",raw:t[0],text:n,href:s,tokens:[{type:"text",raw:n,text:n}]}}}url(e){let t;if(t=this.rules.inline.url.exec(e)){let n,s;if(t[2]==="@")n=t[0],s="mailto:"+n;else{let i;do i=t[0],t[0]=this.rules.inline._backpedal.exec(t[0])?.[0]??"";while(i!==t[0]);n=t[0],t[1]==="www."?s="http://"+t[0]:s=t[0]}return{type:"link",raw:t[0],text:n,href:s,tokens:[{type:"text",raw:n,text:n}]}}}inlineText(e){let t=this.rules.inline.text.exec(e);if(t){let n=this.lexer.state.inRawBlock;return{type:"text",raw:t[0],text:t[0],escaped:n}}}};var x=class l{tokens;options;state;tokenizer;inlineQueue;constructor(e){this.tokens=[],this.tokens.links=Object.create(null),this.options=e||w,this.options.tokenizer=this.options.tokenizer||new S,this.tokenizer=this.options.tokenizer,this.tokenizer.options=this.options,this.tokenizer.lexer=this,this.inlineQueue=[],this.state={inLink:!1,inRawBlock:!1,top:!0};let t={other:m,block:B.normal,inline:P.normal};this.options.pedantic?(t.block=B.pedantic,t.inline=P.pedantic):this.options.gfm&&(t.block=B.gfm,this.options.breaks?t.inline=P.breaks:t.inline=P.gfm),this.tokenizer.rules=t}static get rules(){return{block:B,inline:P}}static lex(e,t){return new l(t).lex(e)}static lexInline(e,t){return new l(t).inlineTokens(e)}lex(e){e=e.replace(m.carriageReturn,"\n"),this.blockTokens(e,this.tokens);for(let t=0;t<this.inlineQueue.length;t++){let n=this.inlineQueue[t];this.inlineTokens(n.src,n.tokens)}return this.inlineQueue=[],this.tokens}blockTokens(e,t=[],n=!1){for(this.options.pedantic&&(e=e.replace(m.tabCharGlobal,"    ").replace(m.spaceLine,""));e;){let s;if(this.options.extensions?.block?.some(r=>(s=r.call({lexer:this},e,t))?(e=e.substring(s.raw.length),t.push(s),!0):!1))continue;if(s=this.tokenizer.space(e)){e=e.substring(s.raw.length);let r=t.at(-1);s.raw.length===1&&r!==void 0?r.raw+="\n":t.push(s);continue}if(s=this.tokenizer.code(e)){e=e.substring(s.raw.length);let r=t.at(-1);r?.type==="paragraph"||r?.type==="text"?(r.raw+="\n"+s.raw,r.text+="\n"+s.text,this.inlineQueue.at(-1).src=r.text):t.push(s);continue}if(s=this.tokenizer.fences(e)){e=e.substring(s.raw.length),t.push(s);continue}if(s=this.tokenizer.heading(e)){e=e.substring(s.raw.length),t.push(s);continue}if(s=this.tokenizer.hr(e)){e=e.substring(s.raw.length),t.push(s);continue}if(s=this.tokenizer.blockquote(e)){e=e.substring(s.raw.length),t.push(s);continue}if(s=this.tokenizer.list(e)){e=e.substring(s.raw.length),t.push(s);continue}if(s=this.tokenizer.html(e)){e=e.substring(s.raw.length),t.push(s);continue}if(s=this.tokenizer.def(e)){e=e.substring(s.raw.length);let r=t.at(-1);r?.type==="paragraph"||r?.type==="text"?(r.raw+="\n"+s.raw,r.text+="\n"+s.raw,this.inlineQueue.at(-1).src=r.text):this.tokens.links[s.tag]||(this.tokens.links[s.tag]={href:s.href,title:s.title});continue}if(s=this.tokenizer.table(e)){e=e.substring(s.raw.length),t.push(s);continue}if(s=this.tokenizer.lheading(e)){e=e.substring(s.raw.length),t.push(s);continue}let i=e;if(this.options.extensions?.startBlock){let r=1/0,o=e.slice(1),a;this.options.extensions.startBlock.forEach(c=>{a=c.call({lexer:this},o),typeof a=="number"&&a>=0&&(r=Math.min(r,a))}),r<1/0&&r>=0&&(i=e.substring(0,r+1))}if(this.state.top&&(s=this.tokenizer.paragraph(i))){let r=t.at(-1);n&&r?.type==="paragraph"?(r.raw+="\n"+s.raw,r.text+="\n"+s.text,this.inlineQueue.pop(),this.inlineQueue.at(-1).src=r.text):t.push(s),n=i.length!==e.length,e=e.substring(s.raw.length);continue}if(s=this.tokenizer.text(e)){e=e.substring(s.raw.length);let r=t.at(-1);r?.type==="text"?(r.raw+="\n"+s.raw,r.text+="\n"+s.text,this.inlineQueue.pop(),this.inlineQueue.at(-1).src=r.text):t.push(s);continue}if(e){let r="Infinite loop on byte: "+e.charCodeAt(0);if(this.options.silent){console.error(r);break}else throw new Error(r)}}return this.state.top=!0,t}inline(e,t=[]){return this.inlineQueue.push({src:e,tokens:t}),t}inlineTokens(e,t=[]){let n=e,s=null;if(this.tokens.links){let o=Object.keys(this.tokens.links);if(o.length>0)for(;(s=this.tokenizer.rules.inline.reflinkSearch.exec(n))!=null;)o.includes(s[0].slice(s[0].lastIndexOf("[")+1,-1))&&(n=n.slice(0,s.index)+"["+"a".repeat(s[0].length-2)+"]"+n.slice(this.tokenizer.rules.inline.reflinkSearch.lastIndex))}for(;(s=this.tokenizer.rules.inline.anyPunctuation.exec(n))!=null;)n=n.slice(0,s.index)+"++"+n.slice(this.tokenizer.rules.inline.anyPunctuation.lastIndex);for(;(s=this.tokenizer.rules.inline.blockSkip.exec(n))!=null;)n=n.slice(0,s.index)+"["+"a".repeat(s[0].length-2)+"]"+n.slice(this.tokenizer.rules.inline.blockSkip.lastIndex);let i=!1,r="";for(;e;){i||(r=""),i=!1;let o;if(this.options.extensions?.inline?.some(c=>(o=c.call({lexer:this},e,t))?(e=e.substring(o.raw.length),t.push(o),!0):!1))continue;if(o=this.tokenizer.escape(e)){e=e.substring(o.raw.length),t.push(o);continue}if(o=this.tokenizer.tag(e)){e=e.substring(o.raw.length),t.push(o);continue}if(o=this.tokenizer.link(e)){e=e.substring(o.raw.length),t.push(o);continue}if(o=this.tokenizer.reflink(e,this.tokens.links)){e=e.substring(o.raw.length);let c=t.at(-1);o.type==="text"&&c?.type==="text"?(c.raw+=o.raw,c.text+=o.text):t.push(o);continue}if(o=this.tokenizer.emStrong(e,n,r)){e=e.substring(o.raw.length),t.push(o);continue}if(o=this.tokenizer.codespan(e)){e=e.substring(o.raw.length),t.push(o);continue}if(o=this.tokenizer.br(e)){e=e.substring(o.raw.length),t.push(o);continue}if(o=this.tokenizer.del(e)){e=e.substring(o.raw.length),t.push(o);continue}if(o=this.tokenizer.autolink(e)){e=e.substring(o.raw.length),t.push(o);continue}if(!this.state.inLink&&(o=this.tokenizer.url(e))){e=e.substring(o.raw.length),t.push(o);continue}let a=e;if(this.options.extensions?.startInline){let c=1/0,p=e.slice(1),u;this.options.extensions.startInline.forEach(d=>{u=d.call({lexer:this},p),typeof u=="number"&&u>=0&&(c=Math.min(c,u))}),c<1/0&&c>=0&&(a=e.substring(0,c+1))}if(o=this.tokenizer.inlineText(a)){e=e.substring(o.raw.length),o.raw.slice(-1)!=="_"&&(r=o.raw.slice(-1)),i=!0;let c=t.at(-1);c?.type==="text"?(c.raw+=o.raw,c.text+=o.text):t.push(o);continue}if(e){let c="Infinite loop on byte: "+e.charCodeAt(0);if(this.options.silent){console.error(c);break}else throw new Error(c)}}return t}};var $=class{options;parser;constructor(e){this.options=e||w}space(e){return""}code({text:e,lang:t,escaped:n}){let s=(t||"").match(m.notSpaceStart)?.[0],i=e.replace(m.endingNewline,"")+"\n";return s?'<pre><code class="language-'+R(s)+'">'+(n?i:R(i,!0))+"</code></pre>\n":"<pre><code>"+(n?i:R(i,!0))+"</code></pre>\n"}blockquote({tokens:e}){return"<blockquote>\n"+this.parser.parse(e)+"</blockquote>\n"}html({text:e}){return e}heading({tokens:e,depth:t}){return"<h"+t+">"+this.parser.parseInline(e)+"</h"+t+">\n"}hr(e){return"<hr>\n"}list(e){let t=e.ordered,n=e.start,s="";for(let o=0;o<e.items.length;o++){let a=e.items[o];s+=this.listitem(a)}let i=t?"ol":"ul",r=t&&n!==1?' start="'+n+'"':"";return"<"+i+r+">\n"+s+"</"+i+">\n"}listitem(e){let t="";if(e.task){let n=this.checkbox({checked:!!e.checked});e.loose?e.tokens[0]?.type==="paragraph"?(e.tokens[0].text=n+" "+e.tokens[0].text,e.tokens[0].tokens&&e.tokens[0].tokens.length>0&&e.tokens[0].tokens[0].type==="text"&&(e.tokens[0].tokens[0].text=n+" "+R(e.tokens[0].tokens[0].text),e.tokens[0].tokens[0].escaped=!0)):e.tokens.unshift({type:"text",raw:n+" ",text:n+" ",escaped:!0}):t+=n+" "}return t+=this.parser.parse(e.tokens,!!e.loose),"<li>"+t+"</li>\n"}checkbox({checked:e}){return"<input "+(e?'checked="" ':"")+'disabled="" type="checkbox">'}paragraph({tokens:e}){return"<p>"+this.parser.parseInline(e)+"</p>\n"}table(e){let t="",n="";for(let i=0;i<e.header.length;i++)n+=this.tablecell(e.header[i]);t+=this.tablerow({text:n});let s="";for(let i=0;i<e.rows.length;i++){let r=e.rows[i];n="";for(let o=0;o<r.length;o++)n+=this.tablecell(r[o]);s+=this.tablerow({text:n})}return s&&(s="<tbody>"+s+"</tbody>"),"<table>\n<thead>\n"+t+"</thead>\n"+s+"</table>\n"}tablerow({text:e}){return"<tr>\n"+e+"</tr>\n"}tablecell(e){let t=this.parser.parseInline(e.tokens),n=e.header?"th":"td";return(e.align?"<"+n+' align="'+e.align+'">':("<"+n+">"))+t+"</"+n+">\n"}strong({tokens:e}){return"<strong>"+this.parser.parseInline(e)+"</strong>"}em({tokens:e}){return"<em>"+this.parser.parseInline(e)+"</em>"}codespan({text:e}){return"<code>"+R(e,!0)+"</code>"}br(e){return"<br>"}del({tokens:e}){return"<del>"+this.parser.parseInline(e)+"</del>"}link({href:e,title:t,tokens:n}){let s=this.parser.parseInline(n),i=V(e);if(i===null)return s;e=i;let r='<a href="'+e+'"';return t&&(r+=' title="'+R(t)+'"'),r+=">"+s+"</a>",r}image({href:e,title:t,text:n,tokens:s}){s&&(n=this.parser.parseInline(s,this.parser.textRenderer));let i=V(e);if(i===null)return R(n);e=i;let r='<img src="'+e+'" alt="'+n+'"';return t&&(r+=' title="'+R(t)+'"'),r+=">",r}text(e){return"tokens"in e&&e.tokens?this.parser.parseInline(e.tokens):"escaped"in e&&e.escaped?e.text:R(e.text)}};var _=class{strong({text:e}){return e}em({text:e}){return e}codespan({text:e}){return e}del({text:e}){return e}html({text:e}){return e}text({text:e}){return e}link({text:e}){return""+e}image({text:e}){return""+e}br(){return""}};var b=class l{options;renderer;textRenderer;constructor(e){this.options=e||w,this.options.renderer=this.options.renderer||new $(this.defaults),this.renderer=this.options.renderer,this.renderer.options=this.options,this.renderer.parser=this,this.textRenderer=new _}static parse(e,t){return new l(t).parse(e)}static parseInline(e,t){return new l(t).parseInline(e)}parse(e,t=!0){let n="";for(let s=0;s<e.length;s++){let i=e[s];if(this.options.extensions?.renderers?.[i.type]){let o=i,a=this.options.extensions.renderers[o.type].call({parser:this},o);if(a!==!1||!["space","hr","heading","code","table","blockquote","list","html","paragraph","text"].includes(o.type)){n+=a||"";continue}}let r=i;switch(r.type){case"space":{n+=this.renderer.space(r);continue}case"hr":{n+=this.renderer.hr(r);continue}case"heading":{n+=this.renderer.heading(r);continue}case"code":{n+=this.renderer.code(r);continue}case"table":{n+=this.renderer.table(r);continue}case"blockquote":{n+=this.renderer.blockquote(r);continue}case"list":{n+=this.renderer.list(r);continue}case"html":{n+=this.renderer.html(r);continue}case"paragraph":{n+=this.renderer.paragraph(r);continue}case"text":{let o=r,a=this.renderer.text(o);for(;s+1<e.length&&e[s+1].type==="text";)o=e[++s],a+="\n"+this.renderer.text(o);t?n+=this.renderer.paragraph({type:"paragraph",raw:a,text:a,tokens:[{type:"text",raw:a,text:a,escaped:!0}]}):n+=a;continue}default:{let o='Token with "'+r.type+'" type was not found.';if(this.options.silent)return console.error(o),"";throw new Error(o)}}}return n}parseInline(e,t=this.renderer){let n="";for(let s=0;s<e.length;s++){let i=e[s];if(this.options.extensions?.renderers?.[i.type]){let o=this.options.extensions.renderers[i.type].call({parser:this},i);if(o!==!1||!["escape","html","link","image","strong","em","codespan","br","del","text"].includes(i.type)){n+=o||"";continue}}let r=i;switch(r.type){case"escape":{n+=t.text(r);break}case"html":{n+=t.html(r);break}case"link":{n+=t.link(r);break}case"image":{n+=t.image(r);break}case"strong":{n+=t.strong(r);break}case"em":{n+=t.em(r);break}case"codespan":{n+=t.codespan(r);break}case"br":{n+=t.br(r);break}case"del":{n+=t.del(r);break}case"text":{n+=t.text(r);break}default:{let o='Token with "'+r.type+'" type was not found.';if(this.options.silent)return console.error(o),"";throw new Error(o)}}}return n}};var L=class{options;block;constructor(e){this.options=e||w}static passThroughHooks=new Set(["preprocess","postprocess","processAllTokens"]);preprocess(e){return e}postprocess(e){return e}processAllTokens(e){return e}provideLexer(){return this.block?x.lex:x.lexInline}provideParser(){return this.block?b.parse:b.parseInline}};var E=class{defaults=z();options=this.setOptions;parse=this.parseMarkdown(!0);parseInline=this.parseMarkdown(!1);Parser=b;Renderer=$;TextRenderer=_;Lexer=x;Tokenizer=S;Hooks=L;constructor(...e){this.use(...e)}walkTokens(e,t){let n=[];for(let s of e)switch(n=n.concat(t.call(this,s)),s.type){case"table":{let i=s;for(let r of i.header)n=n.concat(this.walkTokens(r.tokens,t));for(let r of i.rows)for(let o of r)n=n.concat(this.walkTokens(o.tokens,t));break}case"list":{let i=s;n=n.concat(this.walkTokens(i.items,t));break}default:{let i=s;this.defaults.extensions?.childTokens?.[i.type]?this.defaults.extensions.childTokens[i.type].forEach(r=>{let o=i[r].flat(1/0);n=n.concat(this.walkTokens(o,t))}):i.tokens&&(n=n.concat(this.walkTokens(i.tokens,t)))}}return n}use(...e){let t=this.defaults.extensions||{renderers:{},childTokens:{}};return e.forEach(n=>{let s={...n};if(s.async=this.defaults.async||s.async||!1,n.extensions&&(n.extensions.forEach(i=>{if(!i.name)throw new Error("extension name required");if("renderer"in i){let r=t.renderers[i.name];r?t.renderers[i.name]=function(...o){let a=i.renderer.apply(this,o);return a===!1&&(a=r.apply(this,o)),a}:t.renderers[i.name]=i.renderer}if("tokenizer"in i){if(!i.level||i.level!=="block"&&i.level!=="inline")throw new Error("extension level must be 'block' or 'inline'");let r=t[i.level];r?r.unshift(i.tokenizer):t[i.level]=[i.tokenizer],i.start&&(i.level==="block"?t.startBlock?t.startBlock.push(i.start):t.startBlock=[i.start]:i.level==="inline"&&(t.startInline?t.startInline.push(i.start):t.startInline=[i.start]))}"childTokens"in i&&i.childTokens&&(t.childTokens[i.name]=i.childTokens)}),s.extensions=t),n.renderer){let i=this.defaults.renderer||new $(this.defaults);for(let r in n.renderer){if(!(r in i))throw new Error("renderer '"+r+"' does not exist");if(["options","parser"].includes(r))continue;let o=r,a=n.renderer[o],c=i[o];i[o]=(...p)=>{let u=a.apply(i,p);return u===!1&&(u=c.apply(i,p)),u||""}}s.renderer=i}if(n.tokenizer){let i=this.defaults.tokenizer||new S(this.defaults);for(let r in n.tokenizer){if(!(r in i))throw new Error("tokenizer '"+r+"' does not exist");if(["options","rules","lexer"].includes(r))continue;let o=r,a=n.tokenizer[o],c=i[o];i[o]=(...p)=>{let u=a.apply(i,p);return u===!1&&(u=c.apply(i,p)),u}}s.tokenizer=i}if(n.hooks){let i=this.defaults.hooks||new L;for(let r in n.hooks){if(!(r in i))throw new Error("hook '"+r+"' does not exist");if(["options","block"].includes(r))continue;let o=r,a=n.hooks[o],c=i[o];L.passThroughHooks.has(r)?i[o]=p=>{if(this.defaults.async)return Promise.resolve(a.call(i,p)).then(d=>c.call(i,d));let u=a.call(i,p);return c.call(i,u)}:i[o]=(...p)=>{let u=a.apply(i,p);return u===!1&&(u=c.apply(i,p)),u}}s.hooks=i}if(n.walkTokens){let i=this.defaults.walkTokens,r=n.walkTokens;s.walkTokens=function(o){let a=[];return a.push(r.call(this,o)),i&&(a=a.concat(i.call(this,o))),a}}this.defaults={...this.defaults,...s}}),this}setOptions(e){return this.defaults={...this.defaults,...e},this}lexer(e,t){return x.lex(e,t??this.defaults)}parser(e,t){return b.parse(e,t??this.defaults)}parseMarkdown(e){return(n,s)=>{let i={...s},r={...this.defaults,...i},o=this.onError(!!r.silent,!!r.async);if(this.defaults.async===!0&&i.async===!1)return o(new Error("marked(): The async option was set to true by an extension. Remove async: false from the parse options object to return a Promise."));if(typeof n>"u"||n===null)return o(new Error("marked(): input parameter is undefined or null"));if(typeof n!="string")return o(new Error("marked(): input parameter is of type "+Object.prototype.toString.call(n)+", string expected"));r.hooks&&(r.hooks.options=r,r.hooks.block=e);let a=r.hooks?r.hooks.provideLexer():e?x.lex:x.lexInline,c=r.hooks?r.hooks.provideParser():e?b.parse:b.parseInline;if(r.async)return Promise.resolve(r.hooks?r.hooks.preprocess(n):n).then(p=>a(p,r)).then(p=>r.hooks?r.hooks.processAllTokens(p):p).then(p=>r.walkTokens?Promise.all(this.walkTokens(p,r.walkTokens)).then(()=>p):p).then(p=>c(p,r)).then(p=>r.hooks?r.hooks.postprocess(p):p).catch(o);try{r.hooks&&(n=r.hooks.preprocess(n));let p=a(n,r);r.hooks&&(p=r.hooks.processAllTokens(p)),r.walkTokens&&this.walkTokens(p,r.walkTokens);let u=c(p,r);return r.hooks&&(u=r.hooks.postprocess(u)),u}catch(p){return o(p)}}}onError(e,t){return n=>{if(n.message+="\nPlease report this to https://github.com/markedjs/marked.",e){let s="<p>An error occurred:</p><pre>"+R(n.message+"",!0)+"</pre>";return t?Promise.resolve(s):s}if(t)return Promise.reject(n);throw n}}};var M=new E;function k(l,e){return M.parse(l,e)}k.options=k.setOptions=function(l){return M.setOptions(l),k.defaults=M.defaults,N(k.defaults),k};k.getDefaults=z;k.defaults=w;k.use=function(...l){return M.use(...l),k.defaults=M.defaults,N(k.defaults),k};k.walkTokens=function(l,e){return M.walkTokens(l,e)};k.parseInline=M.parseInline;k.Parser=b;k.parser=b.parse;k.Renderer=$;k.TextRenderer=_;k.Lexer=x;k.lexer=x.lex;k.Tokenizer=S;k.Hooks=L;k.parse=k;var it=k.options,ot=k.setOptions,lt=k.use,at=k.walkTokens,ct=k.parseInline,pt=k,ut=b.parse,ht=x.lex;
if(__exports != exports)module.exports = exports;return module.exports}));`

// webPageHTML is the embedded HTML page for Web control
const webPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MiniK - Terminal</title>
    <style>
        @import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;700&display=swap');

        * { box-sizing: border-box; margin: 0; padding: 0; }

        :root {
            --bg-primary: #0d0d0d;
            --bg-secondary: #1a1a1a;
            --bg-tertiary: #252525;
            --border-color: #333;
            --text-primary: #e0e0e0;
            --text-secondary: #888;
            --text-muted: #666;
            --cyan: #00d4ff;
            --green: #4ade80;
            --orange: #f97316;
            --yellow: #fbbf24;
            --red: #f87171;
            --purple: #c084fc;
            --blue: #60a5fa;
        }

        body {
            font-family: 'JetBrains Mono', 'Consolas', 'Monaco', 'Courier New', monospace;
            background: var(--bg-primary);
            color: var(--text-primary);
            height: 100vh;
            display: flex;
            flex-direction: column;
            font-size: 14px;
            line-height: 1.5;
        }

        /* Terminal Header */
        .header {
            background: var(--bg-secondary);
            padding: 8px 16px;
            display: flex;
            justify-content: space-between;
            align-items: center;
            border-bottom: 1px solid var(--border-color);
            font-size: 13px;
        }
        .header-left {
            display: flex;
            align-items: center;
            gap: 8px;
        }
        .header-title {
            color: var(--cyan);
            font-weight: 700;
        }
        .header-title::before {
            content: "🤖 ";
        }
        .header-workspace {
            color: var(--text-secondary);
        }
        .header-workspace span {
            color: var(--yellow);
        }
        .header-model {
            color: var(--text-secondary);
        }
        .header-model span {
            color: var(--purple);
        }
        .header-sep {
            color: var(--text-muted);
            margin: 0 4px;
        }
        .header-right {
            display: flex;
            align-items: center;
            gap: 12px;
        }
        .header-btn {
            padding: 4px 10px;
            background: transparent;
            color: var(--cyan);
            border: 1px solid var(--border-color);
            cursor: pointer;
            font-size: 12px;
            font-family: inherit;
            transition: all 0.15s;
        }
        .header-btn:hover {
            border-color: var(--cyan);
            background: rgba(0, 212, 255, 0.1);
        }
        .header-btn.active {
            background: var(--cyan);
            color: var(--bg-primary);
            border-color: var(--cyan);
        }
        .status {
            font-size: 12px;
            padding: 2px 8px;
            border: 1px solid var(--border-color);
        }
        .status.connected {
            color: var(--green);
            border-color: var(--green);
        }
        .status.disconnected {
            color: var(--red);
            border-color: var(--red);
            cursor: pointer;
        }
        .status.kicked {
            color: var(--red);
            border-color: var(--red);
        }
        .status.running {
            color: var(--yellow);
            border-color: var(--yellow);
            animation: pulse 1.5s infinite;
        }
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }

        /* Messages Area */
        .messages {
            flex: 1;
            overflow-y: auto;
            padding: 12px 16px;
            background: var(--bg-primary);
        }
        .messages::-webkit-scrollbar {
            width: 8px;
        }
        .messages::-webkit-scrollbar-track {
            background: var(--bg-primary);
        }
        .messages::-webkit-scrollbar-thumb {
            background: var(--border-color);
        }
        .messages::-webkit-scrollbar-thumb:hover {
            background: var(--text-muted);
        }

        /* Message Styles - Terminal Look */
        .message {
            margin-bottom: 8px;
            padding: 4px 0;
            max-width: 100%;
            word-wrap: break-word;
        }
        .message-label {
            font-weight: 500;
            margin-right: 8px;
        }
        .message-content {
            margin-left: 2px;
        }

        /* User Message */
        .message.user {
            color: var(--text-primary);
        }
        .message.user .message-label {
            color: var(--cyan);
        }
        .message.user .message-label::before {
            content: "You";
        }
        .message.user .message-label::after {
            content: " ›";
            color: var(--cyan);
        }

        /* Assistant Message */
        .message.assistant {
            color: var(--text-primary);
        }
        .message.assistant .message-label {
            color: var(--green);
        }
        .message.assistant .message-label::before {
            content: "🤖 Assistant";
        }
        .message.assistant .message-label::after {
            content: " ›";
            color: var(--green);
        }

        /* Thinking Message */
        .message.thinking {
            color: var(--text-secondary);
        }
        .message.thinking .message-label {
            color: var(--orange);
        }
        .message.thinking .message-label::before {
            content: "🍊 Thinking";
        }
        .message.thinking .message-label::after {
            content: " ›";
            color: var(--orange);
        }

        /* System Message */
        .message.system {
            color: var(--text-muted);
            font-style: italic;
        }
        .message.system .message-label {
            color: var(--text-muted);
        }
        .message.system .message-label::before {
            content: "[System]";
        }

        /* Tool Call Message */
        .message.tool_call {
            background: rgba(249, 115, 22, 0.08);
            border-left: 2px solid var(--orange);
            padding: 8px 12px;
            margin: 8px 0;
        }
        .message.tool_call .message-label {
            color: var(--orange);
            display: block;
            cursor: pointer;
            user-select: none;
        }
        .message.tool_call .message-label::before {
            content: "▶ 🔧 ";
        }
        .message.tool_call .message-label.expanded::before {
            content: "▼ 🔧 ";
        }
        .message.tool_call pre {
            background: var(--bg-secondary);
            padding: 8px;
            margin-top: 4px;
            border: 1px solid var(--border-color);
            font-size: 12px;
            overflow-x: auto;
            display: none;
        }
        .message.tool_call pre.show {
            display: block;
        }

        /* Tool Result Message */
        .message.tool_result {
            background: rgba(74, 222, 128, 0.08);
            border-left: 2px solid var(--green);
            padding: 8px 12px;
            margin: 8px 0;
        }
        .message.tool_result .message-label {
            color: var(--green);
            display: block;
            cursor: pointer;
            user-select: none;
        }
        .message.tool_result .message-label::before {
            content: "▶ ✓ Result";
        }
        .message.tool_result .message-label.expanded::before {
            content: "▼ ✓ Result";
        }
        .message.tool_result.error {
            background: rgba(248, 113, 113, 0.08);
            border-left-color: var(--red);
        }
        .message.tool_result.error .message-label {
            color: var(--red);
        }
        .message.tool_result.error .message-label::before {
            content: "▶ ✗ Result";
        }
        .message.tool_result.error .message-label.expanded::before {
            content: "▼ ✗ Result";
        }
        .message.tool_result pre {
            background: var(--bg-secondary);
            padding: 8px;
            margin-top: 4px;
            border: 1px solid var(--border-color);
            font-size: 12px;
            overflow-x: auto;
            max-height: 300px;
            display: none;
        }
        .message.tool_result pre.show {
            display: block;
        }

        /* Todo List */
        .todos {
            background: var(--bg-secondary);
            padding: 8px 16px;
            border-top: 1px solid var(--border-color);
            max-height: 120px;
            overflow-y: auto;
            font-size: 13px;
        }
        .todos.empty { display: none; }
        .todo-item {
            padding: 3px 0;
            display: flex;
            align-items: center;
            gap: 8px;
        }
        .todo-item.completed {
            color: var(--text-muted);
            text-decoration: line-through;
        }
        .todo-item.in_progress {
            color: var(--green);
        }
        .todo-item.in_progress::before {
            content: "▶";
            color: var(--green);
            animation: blink 1s infinite;
        }
        .todo-item.pending {
            color: var(--text-secondary);
        }
        @keyframes blink {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.3; }
        }

        /* Input Area - Terminal Style */
        .input-area {
            background: var(--bg-secondary);
            padding: 12px 16px;
            border-top: 1px solid var(--border-color);
            display: flex;
            gap: 8px;
            align-items: center;
        }
        .input-wrapper {
            flex: 1;
            display: flex;
            align-items: center;
            border: 1px solid var(--cyan);
            background: var(--bg-primary);
            padding: 0 12px;
        }
        .input-prompt {
            color: var(--cyan);
            margin-right: 8px;
            user-select: none;
        }
        .input-area input {
            flex: 1;
            padding: 10px 0;
            border: none;
            background: transparent;
            color: var(--text-primary);
            font-size: 14px;
            font-family: inherit;
        }
        .input-area input:focus {
            outline: none;
        }
        .input-area input::placeholder {
            color: var(--text-muted);
        }
        .input-area button {
            padding: 10px 20px;
            background: var(--cyan);
            color: var(--bg-primary);
            border: none;
            font-weight: 700;
            cursor: pointer;
            font-family: inherit;
            font-size: 13px;
            transition: all 0.15s;
        }
        .input-area button:hover {
            background: #00b8e6;
        }
        .input-area button:disabled {
            background: var(--text-muted);
            cursor: not-allowed;
        }

        /* Command Menu */
        .cmd-menu-container {
            position: relative;
        }
        .cmd-btn {
            padding: 10px 14px;
            background: var(--bg-tertiary);
            color: var(--cyan);
            border: 1px solid var(--border-color);
            font-weight: 500;
            cursor: pointer;
            font-size: 13px;
            font-family: inherit;
            transition: all 0.15s;
        }
        .cmd-btn:hover {
            border-color: var(--cyan);
            background: var(--bg-secondary);
        }
        .cmd-menu {
            display: none;
            position: absolute;
            bottom: 100%;
            left: 0;
            margin-bottom: 8px;
            background: var(--bg-secondary);
            border: 1px solid var(--border-color);
            min-width: 200px;
            box-shadow: 0 -4px 20px rgba(0,0,0,0.5);
            z-index: 100;
        }
        .cmd-menu.active { display: block; }
        .cmd-menu-item {
            padding: 10px 14px;
            cursor: pointer;
            display: flex;
            align-items: center;
            gap: 12px;
            color: var(--text-primary);
            font-size: 13px;
            border-bottom: 1px solid var(--border-color);
            transition: background 0.1s;
        }
        .cmd-menu-item:last-child { border-bottom: none; }
        .cmd-menu-item:hover {
            background: var(--bg-tertiary);
        }
        .cmd-menu-item .cmd-name {
            color: var(--cyan);
            min-width: 80px;
        }
        .cmd-menu-item .cmd-desc {
            color: var(--text-secondary);
            font-size: 12px;
        }

        /* Footer Status Bar */

        pre {
            white-space: pre-wrap;
            word-wrap: break-word;
        }

        /* Markdown styles for assistant messages */
        .message.assistant .md-content {
            margin-left: 2px;
        }
        .message.assistant .md-content h1,
        .message.assistant .md-content h2,
        .message.assistant .md-content h3 {
            margin: 12px 0 8px 0;
            color: var(--cyan);
            font-weight: 700;
        }
        .message.assistant .md-content h1 { font-size: 1.3em; }
        .message.assistant .md-content h2 { font-size: 1.15em; }
        .message.assistant .md-content h3 { font-size: 1.05em; }
        .message.assistant .md-content p {
            margin: 6px 0;
            line-height: 1.6;
        }
        .message.assistant .md-content code {
            background: var(--bg-tertiary);
            padding: 2px 6px;
            font-size: 0.9em;
            color: var(--yellow);
        }
        .message.assistant .md-content pre {
            background: var(--bg-secondary);
            border: 1px solid var(--border-color);
            padding: 10px;
            margin: 8px 0;
            overflow-x: auto;
        }
        .message.assistant .md-content pre code {
            background: none;
            padding: 0;
            color: var(--text-primary);
        }
        .message.assistant .md-content ul,
        .message.assistant .md-content ol {
            margin: 6px 0;
            padding-left: 20px;
        }
        .message.assistant .md-content li {
            margin: 3px 0;
            line-height: 1.5;
        }
        .message.assistant .md-content li::marker {
            color: var(--cyan);
        }
        .message.assistant .md-content blockquote {
            border-left: 2px solid var(--cyan);
            margin: 8px 0;
            padding-left: 12px;
            color: var(--text-secondary);
        }
        .message.assistant .md-content a {
            color: var(--cyan);
            text-decoration: none;
        }
        .message.assistant .md-content a:hover {
            text-decoration: underline;
        }
        .message.assistant .md-content table {
            border-collapse: collapse;
            margin: 8px 0;
            width: 100%;
        }
        .message.assistant .md-content th,
        .message.assistant .md-content td {
            border: 1px solid var(--border-color);
            padding: 6px 10px;
            text-align: left;
        }
        .message.assistant .md-content th {
            background: var(--bg-secondary);
            color: var(--cyan);
        }
        .message.assistant .md-content hr {
            border: none;
            border-top: 1px solid var(--border-color);
            margin: 12px 0;
        }
        .message.assistant .md-content strong {
            color: #fff;
            font-weight: 700;
        }
        .message.assistant .md-content em {
            color: var(--text-secondary);
        }

        /* File Manager Styles */
        .file-manager {
            flex: 1;
            display: none;
            flex-direction: column;
            overflow: hidden;
            background: var(--bg-primary);
        }
        .file-manager.active { display: flex; }
        .messages.hidden, .todos.hidden, .input-area.hidden { display: none; }

        .breadcrumb {
            padding: 8px 16px;
            background: var(--bg-secondary);
            font-size: 13px;
            display: flex;
            align-items: center;
            gap: 4px;
            border-bottom: 1px solid var(--border-color);
        }
        .breadcrumb a {
            color: var(--cyan);
            text-decoration: none;
            cursor: pointer;
        }
        .breadcrumb a:hover { text-decoration: underline; }
        .breadcrumb span { color: var(--text-muted); }

        .file-list {
            flex: 1;
            overflow-y: auto;
            padding: 8px;
        }
        .file-item {
            display: flex;
            align-items: center;
            padding: 8px 12px;
            cursor: pointer;
            gap: 12px;
            border-bottom: 1px solid var(--border-color);
            transition: background 0.1s;
        }
        .file-item:hover {
            background: var(--bg-secondary);
        }
        .file-icon {
            font-size: 16px;
            width: 24px;
            text-align: center;
        }
        .file-info { flex: 1; }
        .file-name {
            font-size: 13px;
            color: var(--text-primary);
        }
        .file-item:hover .file-name {
            color: var(--cyan);
        }
        .file-meta {
            font-size: 11px;
            color: var(--text-muted);
        }
        .file-delete-btn {
            opacity: 0.5;
            background: transparent;
            border: 1px solid transparent;
            color: var(--text-muted);
            cursor: pointer;
            padding: 4px 8px;
            font-size: 14px;
            transition: all 0.15s;
            flex-shrink: 0;
        }
        .file-item:hover .file-delete-btn {
            opacity: 1;
        }
        .file-delete-btn:hover {
            color: var(--red);
            border-color: var(--red);
            background: rgba(248, 113, 113, 0.1);
        }

        .file-actions {
            padding: 10px 16px;
            background: var(--bg-secondary);
            border-top: 1px solid var(--border-color);
            display: flex;
            gap: 8px;
        }
        .file-actions button {
            padding: 8px 16px;
            background: var(--cyan);
            color: var(--bg-primary);
            border: none;
            cursor: pointer;
            font-weight: 700;
            font-family: inherit;
            font-size: 12px;
            transition: all 0.15s;
        }
        .file-actions button:hover {
            background: #00b8e6;
        }

        /* Preview Modal */
        .preview-modal {
            display: none;
            position: fixed;
            top: 0; left: 0; right: 0; bottom: 0;
            background: rgba(0,0,0,0.9);
            z-index: 1000;
            justify-content: center;
            align-items: center;
        }
        .preview-modal.active { display: flex; }
        .preview-content {
            background: var(--bg-primary);
            border: 1px solid var(--border-color);
            max-width: 90%;
            max-height: 90%;
            overflow: auto;
            position: relative;
        }
        .preview-header {
            padding: 10px 16px;
            background: var(--bg-secondary);
            display: flex;
            justify-content: space-between;
            align-items: center;
            position: sticky;
            top: 0;
            border-bottom: 1px solid var(--border-color);
        }
        .preview-header h3 {
            font-size: 13px;
            color: var(--cyan);
            font-weight: 500;
        }
        .preview-close {
            background: none;
            border: none;
            color: var(--text-muted);
            font-size: 20px;
            cursor: pointer;
            padding: 0 4px;
        }
        .preview-close:hover { color: var(--red); }
        .preview-body {
            padding: 16px;
        }
        .preview-body pre {
            white-space: pre-wrap;
            word-wrap: break-word;
            font-size: 13px;
            max-height: 60vh;
            overflow: auto;
            color: var(--text-primary);
        }
        .preview-body img {
            max-width: 100%;
            height: auto;
        }

        /* Markdown styles for preview */
        .preview-body .md-preview {
            line-height: 1.6;
            color: var(--text-primary);
        }
        .preview-body .md-preview h1,
        .preview-body .md-preview h2,
        .preview-body .md-preview h3 {
            margin: 16px 0 8px 0;
            color: var(--cyan);
        }
        .preview-body .md-preview h1 {
            font-size: 1.4em;
            border-bottom: 1px solid var(--border-color);
            padding-bottom: 8px;
        }
        .preview-body .md-preview h2 { font-size: 1.2em; }
        .preview-body .md-preview h3 { font-size: 1.1em; }
        .preview-body .md-preview p { margin: 8px 0; }
        .preview-body .md-preview code {
            background: var(--bg-tertiary);
            padding: 2px 6px;
            font-size: 0.9em;
            color: var(--yellow);
        }
        .preview-body .md-preview pre {
            background: var(--bg-secondary);
            border: 1px solid var(--border-color);
            padding: 12px;
            margin: 8px 0;
            overflow-x: auto;
        }
        .preview-body .md-preview pre code {
            background: none;
            padding: 0;
            color: var(--text-primary);
        }
        .preview-body .md-preview ul,
        .preview-body .md-preview ol {
            margin: 8px 0;
            padding-left: 24px;
        }
        .preview-body .md-preview li { margin: 4px 0; }
        .preview-body .md-preview blockquote {
            border-left: 2px solid var(--cyan);
            margin: 8px 0;
            padding-left: 12px;
            color: var(--text-secondary);
        }
        .preview-body .md-preview a {
            color: var(--cyan);
            text-decoration: none;
        }
        .preview-body .md-preview a:hover { text-decoration: underline; }
        .preview-body .md-preview table {
            border-collapse: collapse;
            margin: 8px 0;
            width: 100%;
        }
        .preview-body .md-preview th,
        .preview-body .md-preview td {
            border: 1px solid var(--border-color);
            padding: 6px 10px;
            text-align: left;
        }
        .preview-body .md-preview th {
            background: var(--bg-secondary);
            color: var(--cyan);
        }
        .preview-body .md-preview hr {
            border: none;
            border-top: 1px solid var(--border-color);
            margin: 12px 0;
        }

        /* Scanline effect (optional retro look) */
        body::before {
            content: "";
            position: fixed;
            top: 0;
            left: 0;
            width: 100%;
            height: 100%;
            pointer-events: none;
            background: repeating-linear-gradient(
                0deg,
                rgba(0, 0, 0, 0.03),
                rgba(0, 0, 0, 0.03) 1px,
                transparent 1px,
                transparent 2px
            );
            z-index: 9999;
        }
    </style>
</head>
<body>
    <div class="header">
        <div class="header-left">
            <span class="header-title">MiniK</span>
            <button id="chatBtn" class="header-btn active" onclick="showChat()">Chat</button>
            <button id="filesBtn" class="header-btn" onclick="showFiles()">Workspace</button>
        </div>
        <div class="header-right">
            <span id="status" class="status disconnected">Connecting...</span>
        </div>
    </div>
    <div id="messages" class="messages"></div>
    <div id="todos" class="todos empty"></div>
    <div class="input-area" id="inputArea">
        <div class="cmd-menu-container">
            <button class="cmd-btn" onclick="toggleCmdMenu(event)">/cmd</button>
            <div id="cmdMenu" class="cmd-menu">
                <div class="cmd-menu-item" onclick="sendCommand('/help')">
                    <span class="cmd-name">/help</span>
                    <span class="cmd-desc">Show help</span>
                </div>
                <div class="cmd-menu-item" onclick="sendCommand('/clear')">
                    <span class="cmd-name">/clear</span>
                    <span class="cmd-desc">Clear history</span>
                </div>
                <div class="cmd-menu-item" onclick="sendCommand('/history')">
                    <span class="cmd-name">/history</span>
                    <span class="cmd-desc">Message count</span>
                </div>
                <div class="cmd-menu-item" onclick="sendCommand('/stats')">
                    <span class="cmd-name">/stats</span>
                    <span class="cmd-desc">Session stats</span>
                </div>
                <div class="cmd-menu-item" onclick="sendCommand('/prompt')">
                    <span class="cmd-name">/prompt</span>
                    <span class="cmd-desc">System prompt</span>
                </div>
            </div>
        </div>
        <div class="input-wrapper">
            <span class="input-prompt">&gt;</span>
            <input type="text" id="input" placeholder="Write your message here... (Press Enter to submit)" autocomplete="off">
        </div>
        <button id="send" onclick="sendMessage()">Send</button>
    </div>
    <!-- File Manager -->
    <div id="fileManager" class="file-manager">
        <div class="breadcrumb" id="breadcrumb"></div>
        <div class="file-list" id="fileList"></div>
        <div class="file-actions">
            <button onclick="document.getElementById('fileUpload').click()">+ Add File</button>
            <input type="file" id="fileUpload" style="display:none" onchange="uploadFile(this)">
            <button onclick="refreshFiles()">Refresh</button>
        </div>
    </div>
    <!-- Preview Modal -->
    <div id="previewModal" class="preview-modal" onclick="if(event.target===this)closePreview()">
        <div class="preview-content">
            <div class="preview-header">
                <h3 id="previewTitle">File Preview</h3>
                <button class="preview-close" onclick="closePreview()">&times;</button>
            </div>
            <div class="preview-body" id="previewBody"></div>
        </div>
    </div>

    <script>
        // Initialize marked.js library
        ` + markedJS + `

        // Configure marked options
        marked.setOptions({
            breaks: true,  // Convert \n to <br>
            gfm: true      // GitHub Flavored Markdown
        });

        // Render markdown content safely
        function renderMarkdown(text) {
            try {
                return marked.parse(text);
            } catch (e) {
                console.error('Markdown parse error:', e);
                return escapeHtml(text);
            }
        }

        let ws;
        let isConnected = false;
        let isRunning = false;
        let isKicked = false;

        // Get secret from URL query parameter
        function getSecret() {
            const params = new URLSearchParams(location.search);
            return params.get('secret') || '';
        }

        function connect() {
            if (isKicked) return; // kicked clients cannot reconnect
            const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
            const secret = getSecret();
            ws = new WebSocket(protocol + '//' + location.host + '/ws?secret=' + encodeURIComponent(secret));

            ws.onopen = () => {
                isConnected = true;
                isKicked = false;
                updateStatus();
            };

            ws.onclose = (e) => {
                isConnected = false;
                if (e.code === 1001) {
                    // Kicked by another client
                    isKicked = true;
                }
                updateStatus();
            };

            ws.onmessage = (e) => {
                const event = JSON.parse(e.data);
                handleEvent(event);
            };
        }

        function handleEvent(event) {
            switch (event.type) {
                case 'snapshot':
                    renderSnapshot(event.data);
                    break;
                case 'message':
                    appendMessage(event.data);
                    break;
                case 'message_update':
                    updateLastMessage(event.data.content);
                    break;
                case 'status':
                    isRunning = event.data.isRunning;
                    updateStatus();
                    break;
                case 'todos':
                    renderTodos(event.data);
                    break;
                case 'clear':
                    document.getElementById('messages').innerHTML = '';
                    break;
            }
        }

        function renderSnapshot(data) {
            const container = document.getElementById('messages');
            container.innerHTML = '';
            data.messages.forEach(msg => appendMessage(msg));
            renderTodos(data.todos);
            isRunning = data.isRunning;
            updateStatus();
        }

        function appendMessage(msg) {
            const container = document.getElementById('messages');
            const div = document.createElement('div');
            div.className = 'message ' + msg.type;
            if (msg.type === 'tool_result' && !msg.success) {
                div.className += ' error';
            }

            let content = '';
            if (msg.type === 'tool_call') {
                content = '<span class="message-label" onclick="toggleToolContent(this)">' + escapeHtml(msg.toolName) + '</span>';
                content += '<pre>' + JSON.stringify(msg.toolArgs, null, 2) + '</pre>';
            } else if (msg.type === 'tool_result') {
                content = '<span class="message-label" onclick="toggleToolContent(this)"> (' + escapeHtml(msg.toolName) + '):</span>';
                content += '<pre>' + escapeHtml(msg.content) + '</pre>';
            } else if (msg.type === 'assistant') {
                // Render assistant messages with Markdown
                content = '<span class="message-label"></span>';
                content += '<div class="md-content">' + renderMarkdown(msg.content) + '</div>';
            } else if (msg.type === 'thinking') {
                content = '<span class="message-label"></span>';
                content += '<span class="message-content">' + escapeHtml(msg.content) + '</span>';
            } else if (msg.type === 'user') {
                content = '<span class="message-label"></span>';
                content += '<span class="message-content">' + escapeHtml(msg.content) + '</span>';
            } else if (msg.type === 'system') {
                content = '<span class="message-label"></span>';
                content += '<span class="message-content">' + escapeHtml(msg.content).replace(/\n/g, '<br>') + '</span>';
            } else {
                content = escapeHtml(msg.content);
            }

            div.innerHTML = content;
            container.appendChild(div);
            container.scrollTop = container.scrollHeight;
        }

        function toggleToolContent(label) {
            const pre = label.parentElement.querySelector('pre');
            if (pre) {
                pre.classList.toggle('show');
                label.classList.toggle('expanded');
            }
        }

        function updateLastMessage(newContent) {
            const container = document.getElementById('messages');
            const messages = container.querySelectorAll('.message');
            if (messages.length === 0) return;

            const lastMsg = messages[messages.length - 1];
            // Only update text-based messages (assistant, thinking, system)
            // Don't update tool_call or tool_result which have special formatting
            if (!lastMsg.classList.contains('tool_call') && !lastMsg.classList.contains('tool_result')) {
                if (lastMsg.classList.contains('assistant')) {
                    // Render assistant messages with Markdown
                    lastMsg.innerHTML = '<span class="message-label"></span><div class="md-content">' + renderMarkdown(newContent) + '</div>';
                } else if (lastMsg.classList.contains('thinking')) {
                    lastMsg.innerHTML = '<span class="message-label"></span><span class="message-content">' + escapeHtml(newContent) + '</span>';
                } else {
                    lastMsg.innerHTML = '<span class="message-label"></span><span class="message-content">' + escapeHtml(newContent) + '</span>';
                }
                container.scrollTop = container.scrollHeight;
            }
        }

        function renderTodos(todos) {
            const container = document.getElementById('todos');
            if (!todos || todos.length === 0) {
                container.className = 'todos empty';
                return;
            }
            container.className = 'todos';
            container.innerHTML = todos.map(todo => {
                const icon = todo.status === 'completed' ? '☒' : '☐';
                return '<div class="todo-item ' + todo.status + '">' + icon + ' ' + escapeHtml(todo.content) + '</div>';
            }).join('');
        }

        function updateStatus() {
            const el = document.getElementById('status');
            if (!isConnected && isKicked) {
                el.textContent = 'Kicked';
                el.className = 'status kicked';
                el.title = 'Replaced by another client';
                el.onclick = null;
            } else if (!isConnected) {
                el.textContent = 'Offline (click to reconnect)';
                el.className = 'status disconnected';
                el.title = 'Click to reconnect';
                el.onclick = connect;
            } else if (isRunning) {
                el.textContent = 'Running...';
                el.className = 'status running';
                el.title = '';
                el.onclick = null;
            } else {
                el.textContent = 'Online';
                el.className = 'status connected';
                el.title = '';
                el.onclick = null;
            }
        }

        function sendMessage() {
            const input = document.getElementById('input');
            const text = input.value.trim();
            if (!text || !isConnected) return;

            ws.send(JSON.stringify({
                type: text.startsWith('/') ? 'command' : 'input',
                content: text
            }));
            input.value = '';
        }

        // ========== Command Menu ==========
        function toggleCmdMenu(event) {
            event.stopPropagation();
            const menu = document.getElementById('cmdMenu');
            menu.classList.toggle('active');
        }

        function sendCommand(cmd) {
            if (!isConnected) return;
            ws.send(JSON.stringify({
                type: 'command',
                content: cmd
            }));
            document.getElementById('cmdMenu').classList.remove('active');
        }

        // Close menu when clicking outside
        document.addEventListener('click', function(e) {
            const menu = document.getElementById('cmdMenu');
            const btn = e.target.closest('.cmd-btn');
            if (!btn && menu.classList.contains('active')) {
                menu.classList.remove('active');
            }
        });

        function escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }

        // ========== File Manager ==========
        let currentPath = '';

        function showChat() {
            document.getElementById('messages').classList.remove('hidden');
            document.getElementById('todos').classList.remove('hidden');
            document.getElementById('inputArea').classList.remove('hidden');
            document.getElementById('fileManager').classList.remove('active');
            document.getElementById('chatBtn').classList.add('active');
            document.getElementById('filesBtn').classList.remove('active');
        }

        function showFiles() {
            document.getElementById('messages').classList.add('hidden');
            document.getElementById('todos').classList.add('hidden');
            document.getElementById('inputArea').classList.add('hidden');
            document.getElementById('fileManager').classList.add('active');
            document.getElementById('chatBtn').classList.remove('active');
            document.getElementById('filesBtn').classList.add('active');
            loadFiles('');
        }

        function loadFiles(path) {
            currentPath = path;
            const secret = getSecret();
            fetch('/api/files?path=' + encodeURIComponent(path) + '&secret=' + encodeURIComponent(secret))
                .then(r => r.json())
                .then(data => {
                    renderBreadcrumb(data.path);
                    renderFileList(data.files);
                })
                .catch(e => console.error('Failed to load files:', e));
        }

        function refreshFiles() {
            loadFiles(currentPath);
        }

        function renderBreadcrumb(path) {
            const bc = document.getElementById('breadcrumb');
            let html = '<a onclick="loadFiles(\'\')">📁 Workspace</a>';
            if (path) {
                const parts = path.split(/[\/\\]/);
                let accumulated = '';
                parts.forEach((part, i) => {
                    accumulated += (i > 0 ? '/' : '') + part;
                    const p = accumulated;
                    html += ' <span>/</span> <a onclick="loadFiles(\'' + escapeHtml(p) + '\')">' + escapeHtml(part) + '</a>';
                });
            }
            bc.innerHTML = html;
        }

        function renderFileList(files) {
            const list = document.getElementById('fileList');
            if (!files || files.length === 0) {
                list.innerHTML = '<div style="padding:20px;color:#888;text-align:center">Empty directory</div>';
                return;
            }
            // Sort: directories first, then files
            files.sort((a, b) => {
                if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
                return a.name.localeCompare(b.name);
            });
            list.innerHTML = files.map(f => {
                const icon = f.isDir ? '📁' : getFileIcon(f.name);
                const size = f.isDir ? '' : formatSize(f.size);
                const filePath = escapeHtml(currentPath ? currentPath + '/' + f.name : f.name);
                const onclick = f.isDir
                    ? 'loadFiles(\'' + filePath + '\')'
                    : 'previewFile(\'' + filePath + '\')';
                return '<div class="file-item" onclick="' + onclick + '">' +
                    '<span class="file-icon">' + icon + '</span>' +
                    '<div class="file-info">' +
                    '<div class="file-name">' + escapeHtml(f.name) + '</div>' +
                    '<div class="file-meta">' + size + '</div>' +
                    '</div>' +
                    '<button class="file-delete-btn" title="Delete" onclick="event.stopPropagation();deleteFile(\'' + filePath + '\',\'' + escapeHtml(f.name) + '\',' + f.isDir + ')">🗑️</button>' +
                    '</div>';
            }).join('');
        }

        function getFileIcon(name) {
            const ext = name.split('.').pop().toLowerCase();
            const icons = {
                'go': '🔵', 'py': '🐍', 'js': '🟡', 'ts': '🔷', 'jsx': '⚛️', 'tsx': '⚛️',
                'html': '🌐', 'css': '🎨', 'json': '📋', 'xml': '📄', 'yaml': '📄', 'yml': '📄',
                'md': '📝', 'txt': '📄', 'log': '📋',
                'png': '🖼️', 'jpg': '🖼️', 'jpeg': '🖼️', 'gif': '🖼️', 'svg': '🖼️', 'webp': '🖼️',
                'mp3': '🎵', 'wav': '🎵', 'mp4': '🎬', 'webm': '🎬',
                'zip': '📦', 'tar': '📦', 'gz': '📦', 'rar': '📦',
                'pdf': '📕', 'doc': '📘', 'docx': '📘', 'xls': '📗', 'xlsx': '📗'
            };
            return icons[ext] || '📄';
        }

        function formatSize(bytes) {
            if (bytes < 1024) return bytes + ' B';
            if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
            if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(1) + ' MB';
            return (bytes / 1024 / 1024 / 1024).toFixed(1) + ' GB';
        }

        function previewFile(path) {
            const secret = getSecret();
            const url = '/api/files/content?path=' + encodeURIComponent(path) + '&secret=' + encodeURIComponent(secret);
            const ext = path.split('.').pop().toLowerCase();
            const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico'];
            const textExts = ['txt', 'md', 'go', 'py', 'js', 'ts', 'jsx', 'tsx', 'html', 'css', 'json', 'xml', 'yaml', 'yml', 'log', 'sh', 'bat', 'rs', 'c', 'cpp', 'h', 'java', 'rb', 'php'];

            document.getElementById('previewTitle').textContent = path.split('/').pop();
            const body = document.getElementById('previewBody');

            if (imageExts.includes(ext)) {
                body.innerHTML = '<img src="' + url + '" alt="Preview">';
            } else if (ext === 'md') {
                // Markdown files - render with markdown
                fetch(url)
                    .then(r => r.text())
                    .then(text => {
                        body.innerHTML = '<div class="md-preview">' + renderMarkdown(text) + '</div>';
                    })
                    .catch(e => {
                        body.innerHTML = '<p style="color:#f87171">Failed to load file</p>';
                    });
            } else if (textExts.includes(ext)) {
                fetch(url)
                    .then(r => r.text())
                    .then(text => {
                        body.innerHTML = '<pre>' + escapeHtml(text) + '</pre>';
                    })
                    .catch(e => {
                        body.innerHTML = '<p style="color:#f87171">Failed to load file</p>';
                    });
            } else if (ext === 'pdf') {
                body.innerHTML = '<iframe src="' + url + '" style="width:80vw;height:70vh;border:none"></iframe>';
            } else {
                // Binary file - offer download
                body.innerHTML = '<p>Binary file - <a href="' + url + '" download style="color:#00d4ff">Download</a></p>';
            }

            document.getElementById('previewModal').classList.add('active');
        }

        function closePreview() {
            document.getElementById('previewModal').classList.remove('active');
            document.getElementById('previewBody').innerHTML = '';
        }

        function uploadFile(input) {
            if (!input.files || !input.files[0]) return;
            const file = input.files[0];
            const formData = new FormData();
            formData.append('file', file);

            const secret = getSecret();
            fetch('/api/files/upload?path=' + encodeURIComponent(currentPath) + '&secret=' + encodeURIComponent(secret), {
                method: 'POST',
                body: formData
            })
            .then(r => r.json())
            .then(data => {
                if (data.success) {
                    refreshFiles();
                } else {
                    alert('Upload failed: ' + (data.error || 'Unknown error'));
                }
            })
            .catch(e => {
                alert('Upload failed: ' + e.message);
            });

            input.value = ''; // Reset file input
        }

        function deleteFile(path, name, isDir) {
            const typeLabel = isDir ? 'directory' : 'file';
            if (!confirm('Delete ' + typeLabel + ' "' + name + '"?')) return;
            const secret = getSecret();
            fetch('/api/files/delete?path=' + encodeURIComponent(path) + '&secret=' + encodeURIComponent(secret), {
                method: 'POST'
            })
            .then(r => r.json())
            .then(data => {
                if (data.success) {
                    refreshFiles();
                } else {
                    alert('Delete failed: ' + (data.error || 'Unknown error'));
                }
            })
            .catch(e => {
                alert('Delete failed: ' + e.message);
            });
        }

        // Handle Enter key
        document.getElementById('input').addEventListener('keypress', (e) => {
            if (e.key === 'Enter') sendMessage();
        });

        // Start connection
        connect();
    </script>
</body>
</html>
`
