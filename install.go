package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var allAgents = []string{"claude", "codex", "cursor", "gemini", "hermes"}

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".claude")
}

func codexDir() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".codex")
}

func agentFile(agent string) string {
	switch agent {
	case "claude":
		return filepath.Join(claudeDir(), "settings.json")
	case "codex":
		return filepath.Join(codexDir(), "config.toml")
	case "cursor":
		return filepath.Join(homeDir(), ".cursor", "hooks.json")
	case "gemini":
		return filepath.Join(homeDir(), ".gemini", "settings.json")
	case "hermes":
		return filepath.Join(hermesDir(), "config.yaml")
	}
	return ""
}

func agentFound(agent string) bool {
	if _, err := os.Stat(filepath.Dir(agentFile(agent))); err == nil {
		return true
	}
	bins := map[string][]string{"claude": {"claude"}, "codex": {"codex"}, "cursor": {"cursor", "cursor-agent"}, "gemini": {"gemini"}, "hermes": {"hermes"}}
	for _, b := range bins[agent] {
		if _, err := exec.LookPath(b); err == nil {
			return true
		}
	}
	return false
}

func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if strings.Contains(p, "go-build") {
		return "", errors.New("run a built binary, not `go run` (the temp path would disappear)")
	}
	return p, nil
}

// hookCmd builds the shell command an agent runs. Claude Code runs hooks through a
// POSIX shell even on Windows (Git Bash), so it gets forward slashes there.
func hookCmd(exe, kind string, posix bool) string {
	if runtime.GOOS == "windows" && posix {
		exe = filepath.ToSlash(exe)
	}
	if strings.ContainsAny(exe, " ()&'") {
		exe = `"` + exe + `"`
	}
	return exe + " hook " + kind
}

func isOurs(cmd string) bool {
	return strings.Contains(cmd, "agent-ping") && strings.Contains(cmd, " hook ")
}

func pickAgents(args []string, onlyFound bool) []string {
	if len(args) > 0 {
		return args
	}
	if !onlyFound {
		return allAgents
	}
	var out []string
	for _, a := range allAgents {
		if agentFound(a) {
			out = append(out, a)
		}
	}
	return out
}

func cmdInstall(args []string) error {
	cfg, _ := loadConfig()
	exe, err := exePath()
	if err != nil {
		return err
	}
	for _, a := range allAgents {
		if len(args) == 0 && !agentFound(a) {
			fmt.Println(T(cfg, "notfound", agentNames[a]))
		}
	}
	for _, a := range pickAgents(args, true) {
		if _, ok := agentNames[a]; !ok {
			fmt.Println(T(cfg, "failed", a, "unknown agent (use: claude codex cursor gemini hermes)"))
			continue
		}
		if err := installAgent(cfg, a, exe); err != nil {
			fmt.Println(T(cfg, "failed", agentNames[a], err))
			continue
		}
		fmt.Println(T(cfg, "hooked", agentNames[a], agentFile(a)))
	}
	fmt.Println(T(cfg, "restart"))
	return nil
}

func cmdUninstall(args []string) error {
	cfg, _ := loadConfig()
	for _, a := range pickAgents(args, false) {
		f := agentFile(a)
		if _, err := os.Stat(f); err != nil {
			continue
		}
		if err := uninstallAgent(cfg, a); err != nil {
			fmt.Println(T(cfg, "failed", agentNames[a], err))
			continue
		}
		fmt.Println(T(cfg, "unhooked", agentNames[a], f))
	}
	return nil
}

// Events we hook per agent: event name -> our hook kind.
var claudeEvents = [][2]string{{"UserPromptSubmit", "claude-start"}, {"Stop", "claude-stop"}, {"Notification", "claude-notify"}}
var geminiEvents = [][2]string{{"BeforeAgent", "gemini-start"}, {"AfterAgent", "gemini-stop"}, {"Notification", "gemini-notify"}}
var cursorEvents = [][2]string{{"beforeSubmitPrompt", "cursor-start"}, {"stop", "cursor-stop"}}

func installAgent(cfg *Config, agent, exe string) error {
	path := agentFile(agent)
	switch agent {
	case "codex":
		return installCodex(cfg, path, exe)
	case "hermes":
		return installHermes(cfg, path, exe)
	}
	root, err := readJSON(path)
	if err != nil {
		return err
	}
	switch agent {
	case "claude":
		for _, e := range claudeEvents {
			addNested(root, e[0], hookCmd(exe, e[1], true))
		}
	case "gemini":
		for _, e := range geminiEvents {
			addNested(root, e[0], hookCmd(exe, e[1], false))
		}
	case "cursor":
		if _, ok := root["version"]; !ok {
			root["version"] = 1
		}
		for _, e := range cursorEvents {
			addFlat(root, e[0], hookCmd(exe, e[1], false))
		}
	}
	return writeJSON(path, root)
}

func uninstallAgent(cfg *Config, agent string) error {
	path := agentFile(agent)
	switch agent {
	case "codex":
		return uninstallCodex(cfg, path)
	case "hermes":
		return uninstallHermes(path)
	}
	root, err := readJSON(path)
	if err != nil {
		return err
	}
	hooks, _ := root["hooks"].(map[string]any)
	for ev := range hooks {
		if agent == "cursor" {
			removeFlat(root, ev)
		} else {
			removeNested(root, ev)
		}
	}
	return writeJSON(path, root)
}

// ---- JSON files (Claude, Gemini, Cursor) ----

func readJSON(path string) (map[string]any, error) {
	root := map[string]any{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || len(bytes.TrimSpace(data)) == 0 {
		return root, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("can't read %s (%v) — fix it or add the hooks by hand, see README", path, err)
	}
	return root, nil
}

func writeJSON(path string, root map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	backup(path)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		return err
	}
	return writeKeepMode(path, buf.Bytes())
}

// backup keeps the very first version we ever touched, so it's always the user's original.
func backup(path string) {
	b := path + ".agent-ping.bak"
	if _, err := os.Stat(b); err == nil {
		return
	}
	if data, err := os.ReadFile(path); err == nil {
		os.WriteFile(b, data, 0o600)
	}
}

func writeKeepMode(path string, data []byte) error {
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}

func hooksMap(root map[string]any) map[string]any {
	h, ok := root["hooks"].(map[string]any)
	if !ok {
		h = map[string]any{}
		root["hooks"] = h
	}
	return h
}

// Claude / Gemini shape: hooks.Event = [ {matcher?, hooks: [ {type, command} ]} ]
func addNested(root map[string]any, event, cmd string) {
	removeNested(root, event)
	h := hooksMap(root)
	list, _ := h[event].([]any)
	h[event] = append(list, map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": cmd}},
	})
}

func removeNested(root map[string]any, event string) {
	h, ok := root["hooks"].(map[string]any)
	if !ok {
		return
	}
	list, _ := h[event].([]any)
	var keep []any
	for _, g := range list {
		gm, ok := g.(map[string]any)
		if !ok {
			keep = append(keep, g)
			continue
		}
		inner, _ := gm["hooks"].([]any)
		var in2 []any
		for _, x := range inner {
			if xm, ok := x.(map[string]any); ok && isOurs(fmt.Sprint(xm["command"])) {
				continue
			}
			in2 = append(in2, x)
		}
		if len(in2) == 0 && len(inner) > 0 {
			continue
		}
		gm["hooks"] = in2
		keep = append(keep, gm)
	}
	setOrDelete(root, h, event, keep)
}

// Cursor shape: hooks.event = [ {command} ]
func addFlat(root map[string]any, event, cmd string) {
	removeFlat(root, event)
	h := hooksMap(root)
	list, _ := h[event].([]any)
	h[event] = append(list, map[string]any{"command": cmd})
}

func removeFlat(root map[string]any, event string) {
	h, ok := root["hooks"].(map[string]any)
	if !ok {
		return
	}
	list, _ := h[event].([]any)
	var keep []any
	for _, x := range list {
		if xm, ok := x.(map[string]any); ok && isOurs(fmt.Sprint(xm["command"])) {
			continue
		}
		keep = append(keep, x)
	}
	setOrDelete(root, h, event, keep)
}

func setOrDelete(root, h map[string]any, event string, keep []any) {
	if len(keep) == 0 {
		delete(h, event)
	} else {
		h[event] = keep
	}
	if len(h) == 0 {
		delete(root, "hooks")
	}
}

// ---- Codex (config.toml, top-level `notify = [...]`) ----

func installCodex(cfg *Config, path, exe string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text := string(data)
	ours := tomlArray([]string{exe, "hook", "codex"})
	start, end, old := findNotify(text)
	if start < 0 {
		text = "notify = " + ours + "\n" + text
	} else {
		if !isOurs(strings.Join(old, " ")) && len(old) > 0 {
			cfg.CodexChain = old
			if err := saveConfig(cfg); err != nil {
				return err
			}
			fmt.Println(T(cfg, "codex_chain"))
		}
		text = text[:start] + "notify = " + ours + text[end:]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	backup(path)
	return writeKeepMode(path, []byte(text))
}

func uninstallCodex(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	start, end, old := findNotify(text)
	if start < 0 || !isOurs(strings.Join(old, " ")) {
		return nil
	}
	if len(cfg.CodexChain) > 0 {
		text = text[:start] + "notify = " + tomlArray(cfg.CodexChain) + text[end:]
		cfg.CodexChain = nil
		saveConfig(cfg)
	} else {
		if end < len(text) && text[end] == '\n' {
			end++
		}
		text = text[:start] + text[end:]
	}
	return writeKeepMode(path, []byte(text))
}

func tomlArray(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		q[i] = `"` + s + `"`
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// findNotify locates a top-level `notify = [...]` (before any [table]).
// Returns byte offsets of the whole assignment (without trailing newline) and its strings.
func findNotify(text string) (int, int, []string) {
	pos := 0
	depth := 0 // bracket depth of a multi-line value we're inside
	for pos < len(text) {
		lineEnd := strings.IndexByte(text[pos:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text)
		} else {
			lineEnd += pos
		}
		line := text[pos:lineEnd]
		trim := strings.TrimSpace(line)
		if depth == 0 {
			if strings.HasPrefix(trim, "[") {
				return -1, -1, nil // first table: no more top-level keys
			}
			if k, _, ok := strings.Cut(trim, "="); ok && strings.TrimSpace(k) == "notify" {
				eq := pos + strings.Index(line, "=")
				end := scanValueEnd(text, eq+1)
				return pos + strings.Index(line, "notify"), end, parseTomlStrings(text[eq+1 : end])
			}
		}
		depth += bracketDelta(line)
		if depth < 0 {
			depth = 0
		}
		pos = lineEnd + 1
	}
	return -1, -1, nil
}

// scanValueEnd returns the end offset of a TOML value starting at i (handles arrays over several lines).
func scanValueEnd(text string, i int) int {
	depth, inStr := 0, byte(0)
	started := false
	for j := i; j < len(text); j++ {
		c := text[j]
		switch {
		case inStr != 0:
			if c == '\\' && inStr == '"' {
				j++
			} else if c == inStr {
				inStr = 0
			}
		case c == '"' || c == '\'':
			inStr, started = c, true
		case c == '[':
			depth++
			started = true
		case c == ']':
			depth--
			if depth == 0 {
				return j + 1
			}
		case c == '#' && depth == 0:
			for j < len(text) && text[j] != '\n' {
				j++
			}
			return j
		case c == '\n' && depth == 0 && started:
			return j
		}
	}
	return len(text)
}

func bracketDelta(line string) int {
	d, inStr := 0, byte(0)
	for j := 0; j < len(line); j++ {
		c := line[j]
		switch {
		case inStr != 0:
			if c == '\\' && inStr == '"' {
				j++
			} else if c == inStr {
				inStr = 0
			}
		case c == '"' || c == '\'':
			inStr = c
		case c == '#':
			return d
		case c == '[':
			d++
		case c == ']':
			d--
		}
	}
	return d
}

// parseTomlStrings pulls the "..." / '...' strings out of a TOML array.
func parseTomlStrings(v string) []string {
	var out []string
	for j := 0; j < len(v); j++ {
		c := v[j]
		if c == '#' {
			for j < len(v) && v[j] != '\n' {
				j++
			}
			continue
		}
		if c != '"' && c != '\'' {
			continue
		}
		var sb strings.Builder
		j++
		for ; j < len(v) && v[j] != c; j++ {
			if c == '"' && v[j] == '\\' && j+1 < len(v) {
				j++
				switch v[j] {
				case 'n':
					sb.WriteByte('\n')
				case 't':
					sb.WriteByte('\t')
				default:
					sb.WriteByte(v[j])
				}
				continue
			}
			sb.WriteByte(v[j])
		}
		out = append(out, sb.String())
	}
	return out
}

func isHooked(agent string) bool {
	data, err := os.ReadFile(agentFile(agent))
	return err == nil && strings.Contains(string(data), "agent-ping") && strings.Contains(string(data), "hook")
}

func hookedAgents() []string {
	var out []string
	for _, a := range allAgents {
		if isHooked(a) {
			out = append(out, a)
		}
	}
	return out
}
