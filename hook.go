package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// runHook is what the agents call. kind is "<agent>-<event>", or plain "codex" for Codex's
// notify program, which passes its JSON as the last argument instead of on stdin.
// It must never print junk to stdout or exit non-zero: that could block the agent.
func runHook(args []string) {
	if len(args) == 0 {
		return
	}
	kind := args[0]
	agent, event, _ := strings.Cut(kind, "-")
	codexNotify := kind == "codex"
	if codexNotify {
		event = "stop"
	}
	// Cursor reads stdout as JSON; answer first so a crash below can't block it.
	if agent == "cursor" {
		if event == "start" {
			fmt.Println(`{"continue":true}`)
		} else {
			fmt.Println(`{}`)
		}
	}
	defer func() { recover() }()

	payload := map[string]any{}
	if codexNotify {
		if len(args) > 1 {
			json.Unmarshal([]byte(args[len(args)-1]), &payload)
		}
	} else {
		data, _ := io.ReadAll(io.LimitReader(os.Stdin, 8<<20))
		json.Unmarshal(data, &payload)
	}
	if agent == "hermes" && !hermesPayload(payload) {
		return
	}
	cfg, _ := loadConfig()
	if cfg.isOff(agent) {
		if codexNotify {
			runCodexChain(cfg, args[1:])
		}
		return
	}

	switch event {
	case "start":
		markStart(agent, sessionKey(payload))
	case "stop":
		if codexNotify {
			runCodexChain(cfg, args[1:])
		}
		finish(cfg, agent, payload)
	case "notify":
		approval(cfg, agent, payload)
	case "ask":
		askingYou(cfg, agent, payload)
	}
}

// hermesPayload lifts Hermes' event fields out of "extra" and maps its outcome flags to our
// status. It returns false for turns you already watch elsewhere: chat gateways (Telegram,
// Slack…), cron, subagents, and approvals decided by the model without asking you.
func hermesPayload(p map[string]any) bool {
	if extra, ok := p["extra"].(map[string]any); ok {
		for k, v := range extra {
			if _, ok := p[k]; !ok {
				p[k] = v
			}
		}
	}
	switch str(p, "platform") {
	case "", "cli", "tui", "desktop":
	default:
		return false
	}
	if str(p, "surface") == "smart" {
		return false
	}
	if p["interrupted"] == true {
		p["status"] = "aborted"
	} else if p["failed"] == true {
		p["status"] = "error"
	}
	if str(p, "message") == "" {
		if d := str(p, "description"); d != "" {
			p["message"] = d
		}
	}
	return true
}

func str(p map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := p[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func sessionKey(p map[string]any) string {
	k := str(p, "session_id", "conversation_id", "thread-id", "thread_id")
	k = unsafeChars.ReplaceAllString(k, "")
	if k == "" {
		k = "default"
	}
	if len(k) > 80 {
		k = k[:80]
	}
	return k
}

func startFile(agent, key string) string {
	return filepath.Join(stateDir(), agent+"-"+key+".start")
}

func markStart(agent, key string) {
	os.MkdirAll(stateDir(), 0o700)
	os.WriteFile(startFile(agent, key), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0o600)
	cleanOldState()
}

// takeStart returns how long ago the task started, if we know.
func takeStart(agent, key string) (time.Duration, bool) {
	f := startFile(agent, key)
	b, err := os.ReadFile(f)
	if err != nil {
		return 0, false
	}
	os.Remove(f)
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, false
	}
	return time.Since(time.Unix(0, n)), true
}

// firstForTurn is false if this turn was already reported: the Codex desktop app can call notify
// twice for one turn. Creating the marker file is atomic, so two racing calls send one ping.
func firstForTurn(agent string, p map[string]any) bool {
	turn := unsafeChars.ReplaceAllString(str(p, "turn-id", "turn_id"), "")
	if turn == "" {
		return true
	}
	os.MkdirAll(stateDir(), 0o700)
	f, err := os.OpenFile(filepath.Join(stateDir(), agent+"-turn-"+turn), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return !os.IsExist(err)
	}
	f.Close()
	return true
}

func cleanOldState() {
	entries, _ := os.ReadDir(stateDir())
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 48*time.Hour {
			os.Remove(filepath.Join(stateDir(), e.Name()))
		}
	}
}

func projectName(p map[string]any) string {
	dir := str(p, "cwd")
	if roots, ok := p["workspace_roots"].([]any); ok && len(roots) > 0 {
		if s, ok := roots[0].(string); ok && s != "" {
			dir = s
		}
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(dir); err == nil { // Codex can report cwd as "."
		dir = abs
	}
	dir = strings.TrimRight(strings.ReplaceAll(dir, "\\", "/"), "/")
	name := filepath.Base(filepath.FromSlash(dir))
	if name == "." || name == "/" || name == `\` || dir == "" {
		return ""
	}
	return name
}

// projectLine is "Project: x", or nothing when the agent didn't say where it ran.
func projectLine(cfg *Config, p map[string]any) []string {
	if name := projectName(p); name != "" {
		return []string{T(cfg, "project", name)}
	}
	return nil
}

// userAway is false when you were at the keyboard recently (if the user asked for that filter).
func userAway(cfg *Config) bool {
	if cfg.IdleSeconds <= 0 {
		return true
	}
	idle, ok := idleSeconds()
	return !ok || idle >= float64(cfg.IdleSeconds)
}

func finish(cfg *Config, agent string, p map[string]any) {
	if !firstForTurn(agent, p) {
		return
	}
	dur, known := takeStart(agent, sessionKey(p))
	status := str(p, "status")
	if status == "aborted" { // you pressed stop yourself, so you're right there
		return
	}
	if known && cfg.MinSeconds > 0 && dur < time.Duration(cfg.MinSeconds)*time.Second {
		return
	}
	if !userAway(cfg) {
		return
	}
	name := agentNames[agent]
	title := T(cfg, "done", name)
	if status == "error" {
		title = T(cfg, "error", name)
	}
	lines := projectLine(cfg, p)
	if known {
		lines = append(lines, T(cfg, "took", fmtDuration(cfg, dur)))
	}
	if cfg.IncludeText {
		txt := str(p, "last-assistant-message", "last_assistant_message", "prompt_response")
		if txt == "" {
			txt = lastClaudeText(str(p, "transcript_path"))
		}
		if txt = snippet(txt, 140); txt != "" {
			lines = append(lines, txt)
		}
	}
	sendAll(cfg, Message{Title: title, Body: strings.Join(lines, "\n")})
}

func approval(cfg *Config, agent string, p map[string]any) {
	if cfg.SkipApproval || !userAway(cfg) {
		return
	}
	msg := str(p, "message")
	if ti, ok := p["tool_input"].(map[string]any); ok && msg == "" { // Codex PermissionRequest
		msg = str(ti, "description")
	}
	nt := str(p, "notification_type")
	// Claude's "waiting for your input" reminder repeats the Stop notification; auth_success isn't a question.
	if nt == "idle_prompt" || nt == "auth_success" || strings.Contains(strings.ToLower(msg), "waiting for your input") {
		return
	}
	if askedRecently(agent, sessionKey(p)) { // Claude follows AskUserQuestion with a permission notice
		return
	}
	title := T(cfg, "approve", agentNames[agent])
	if nt == "elicitation_dialog" { // an MCP server wants you to fill something in
		title = T(cfg, "ask", agentNames[agent])
	}
	lines := projectLine(cfg, p)
	if msg != "" {
		lines = append(lines, snippet(msg, 140))
	}
	sendAll(cfg, Message{Title: title, Body: strings.Join(lines, "\n")})
}

// askingYou is for the agent's "ask the user" tool (Claude's AskUserQuestion, Hermes' clarify, Codex's
// request_user_input): the turn is paused on you, not done. The question itself is only sent
// with include_text, like replies.
func askingYou(cfg *Config, agent string, p map[string]any) {
	if cfg.SkipApproval || !userAway(cfg) {
		return
	}
	os.MkdirAll(stateDir(), 0o700)
	os.WriteFile(askFile(agent, sessionKey(p)), nil, 0o600)
	lines := projectLine(cfg, p)
	if cfg.IncludeText {
		if q := askedQuestion(p); q != "" {
			lines = append(lines, snippet(q, 140))
		}
	}
	sendAll(cfg, Message{Title: T(cfg, "ask", agentNames[agent]), Body: strings.Join(lines, "\n")})
}

func askFile(agent, key string) string { return filepath.Join(stateDir(), agent+"-"+key+".ask") }

// askedRecently reports (once) whether we just sent a "question" ping for this session.
func askedRecently(agent, key string) bool {
	f := askFile(agent, key)
	st, err := os.Stat(f)
	if err != nil {
		return false
	}
	os.Remove(f)
	return time.Since(st.ModTime()) < 30*time.Second
}

func askedQuestion(p map[string]any) string {
	ti, _ := p["tool_input"].(map[string]any)
	if ti == nil {
		ti, _ = p["args"].(map[string]any)
	}
	if qs, ok := ti["questions"].([]any); ok && len(qs) > 0 { // Claude, Codex
		if q, ok := qs[0].(map[string]any); ok {
			return str(q, "question", "header")
		}
	}
	return str(ti, "question", "prompt")
}

func snippet(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// lastClaudeText reads the end of a Claude Code transcript and returns the last assistant text.
func lastClaudeText(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > 512<<10 {
		f.Seek(st.Size()-512<<10, io.SeekStart)
	}
	data, _ := io.ReadAll(f)
	var last string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var e struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Type != "assistant" {
			continue
		}
		for _, c := range e.Message.Content {
			if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
				last = c.Text
			}
		}
	}
	return last
}

// runCodexChain calls the notify program Codex had before agent-ping took the slot.
func runCodexChain(cfg *Config, args []string) {
	if len(cfg.CodexChain) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	all := append(append([]string{}, cfg.CodexChain[1:]...), args...)
	cmd := exec.CommandContext(ctx, cfg.CodexChain[0], all...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	cmd.Run()
}
