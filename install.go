package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
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

// Events we hook per agent: {event name, our hook kind, tool matcher (optional)}.
var claudeEvents = [][3]string{{"UserPromptSubmit", "claude-start"}, {"Stop", "claude-stop"}, {"Notification", "claude-notify"},
	{"PreToolUse", "claude-ask", "^AskUserQuestion$"}}
var geminiEvents = [][3]string{{"BeforeAgent", "gemini-start"}, {"AfterAgent", "gemini-stop"}, {"Notification", "gemini-notify"}}

// Codex's newer hooks (~/.codex/hooks.json) add what notify can't: a start time, approvals and
// questions. Turn completion stays on notify, which every Codex version and app calls.
var codexHookEvents = [][3]string{{"UserPromptSubmit", "codex-start"}, {"PermissionRequest", "codex-notify"},
	{"PreToolUse", "codex-ask", "^request_user_input$"}}
var cursorEvents = [][2]string{{"beforeSubmitPrompt", "cursor-start"}, {"stop", "cursor-stop"}}

func installAgent(cfg *Config, agent, exe string) error {
	path := agentFile(agent)
	switch agent {
	case "codex":
		if err := installCodex(cfg, path, exe); err != nil {
			return err
		}
		return installCodexHooks(cfg, exe)
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
			addNested(root, e[0], e[2], hookCmd(exe, e[1], true))
		}
	case "gemini":
		for _, e := range geminiEvents {
			addNested(root, e[0], e[2], hookCmd(exe, e[1], false))
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
		if err := uninstallCodex(cfg, path); err != nil {
			return err
		}
		return uninstallCodexHooks()
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

// Claude / Gemini / Codex shape: hooks.Event = [ {matcher?, hooks: [ {type, command} ]} ]
func addNested(root map[string]any, event, matcher, cmd string) {
	removeNested(root, event)
	h := hooksMap(root)
	list, _ := h[event].([]any)
	group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd}}}
	if matcher != "" {
		group["matcher"] = matcher
	}
	h[event] = append(list, group)
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

func codexHooksFile() string { return filepath.Join(codexDir(), "hooks.json") }

func installCodexHooks(cfg *Config, exe string) error {
	path := codexHooksFile()
	root, err := readJSON(path)
	if err != nil {
		return err
	}
	for _, e := range codexHookEvents {
		addNested(root, e[0], e[2], hookCmd(exe, e[1], false))
	}
	if err := writeJSON(path, root); err != nil {
		return err
	}
	return trustCodexHooks(root)
}

func uninstallCodexHooks() error {
	path := codexHooksFile()
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	root, err := readJSON(path)
	if err != nil {
		return err
	}
	if err := untrustCodexHooks(root); err != nil {
		return err
	}
	hooks, _ := root["hooks"].(map[string]any)
	for ev := range hooks {
		removeNested(root, ev)
	}
	if len(root) == 0 { // nothing but ours was in there
		return os.Remove(path)
	}
	return writeJSON(path, root)
}

// Codex runs a hooks.json entry only after you trust it (its /hooks screen), recorded in
// config.toml as [hooks.state."<file>:<event>:<group>:<handler>"] trusted_hash = "sha256:…".
// Installing is that consent, so we write the same record Codex would.

// codexEventKey turns "PreToolUse" into Codex's "pre_tool_use".
func codexEventKey(event string) string {
	var b strings.Builder
	for i, r := range event {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// codexHookHash mirrors codex-rs hook_hash: sha256 over the sorted, compact JSON of the
// normalized identity (default timeout 600, async false).
func codexHookHash(event, matcher, cmd string) string {
	ident := map[string]any{
		"event_name": codexEventKey(event),
		"hooks":      []any{map[string]any{"type": "command", "command": cmd, "timeout": 600, "async": false}},
	}
	if matcher != "" {
		ident["matcher"] = matcher
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // serde_json doesn't escape <>&
	enc.Encode(ident)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(bytes.TrimSpace(buf.Bytes())))
}

type codexTrust struct{ key, hash string }

// ourCodexTrust lists the state key and hash of every agent-ping hook in a hooks.json root.
func ourCodexTrust(root map[string]any) []codexTrust {
	var out []codexTrust
	hooks, _ := root["hooks"].(map[string]any)
	for ev, v := range hooks {
		groups, _ := v.([]any)
		for gi, g := range groups {
			gm, _ := g.(map[string]any)
			matcher, _ := gm["matcher"].(string)
			inner, _ := gm["hooks"].([]any)
			for hi, x := range inner {
				xm, _ := x.(map[string]any)
				cmd := fmt.Sprint(xm["command"])
				if !isOurs(cmd) {
					continue
				}
				key := fmt.Sprintf("%s:%s:%d:%d", codexHooksFile(), codexEventKey(ev), gi, hi)
				out = append(out, codexTrust{key, codexHookHash(ev, matcher, cmd)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func trustCodexHooks(root map[string]any) error {
	if err := untrustCodexHooks(root); err != nil {
		return err
	}
	path := agentFile("codex")
	data, _ := os.ReadFile(path)
	text := string(data)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	for _, t := range ourCodexTrust(root) {
		text += "\n[hooks.state." + tomlString(t.key) + "]\ntrusted_hash = " + tomlString(t.hash) + "\n"
	}
	backup(path)
	return writeKeepMode(path, []byte(text))
}

// untrustCodexHooks drops the trust records of our hooks (matched by hash, so a moved index is fine).
func untrustCodexHooks(root map[string]any) error {
	ours := map[string]bool{}
	for _, t := range ourCodexTrust(root) {
		ours[t.hash] = true
	}
	path := agentFile("codex")
	data, err := os.ReadFile(path)
	if err != nil || len(ours) == 0 {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "[hooks.state.") {
			out = append(out, lines[i])
			continue
		}
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[j]), "[") {
			j++
		}
		section := strings.Join(lines[i:j], "\n")
		if m := regexp.MustCompile(`trusted_hash\s*=\s*"([^"]+)"`).FindStringSubmatch(section); m != nil && ours[m[1]] {
			for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" { // the blank line we put before it
				out = out[:len(out)-1]
			}
			if j < len(lines) {
				out = append(out, "")
			}
			i = j - 1
			continue
		}
		out = append(out, lines[i:j]...)
		i = j - 1
	}
	text := strings.Join(out, "\n")
	if strings.HasSuffix(string(data), "\n") && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if text == string(data) {
		return nil
	}
	return writeKeepMode(path, []byte(text))
}

func tomlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func tomlArray(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = tomlString(s)
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
