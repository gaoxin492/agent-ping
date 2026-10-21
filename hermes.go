package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ---- Hermes Agent (config.yaml `hooks:` block + shell-hooks-allowlist.json) ----

// Events we hook in Hermes. pre_llm_call fires once per turn, on_session_end at the end of every turn.
var hermesEvents = [][2]string{{"pre_llm_call", "hermes-start"}, {"on_session_end", "hermes-stop"}, {"pre_approval_request", "hermes-notify"}}

func hermesDir() string {
	if d := os.Getenv("HERMES_HOME"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "hermes")
		}
		return filepath.Join(homeDir(), "AppData", "Local", "hermes")
	}
	return filepath.Join(homeDir(), ".hermes")
}

func hermesAllowlist() string {
	return filepath.Join(filepath.Dir(agentFile("hermes")), "shell-hooks-allowlist.json")
}

var (
	yamlHooksKey = regexp.MustCompile(`^hooks:\s*(#.*)?$`)
	yamlHooksNil = regexp.MustCompile(`^hooks:\s*(\{\}|null|~)\s*(#.*)?$`)
	yamlChildKey = regexp.MustCompile(`^(\s+)([A-Za-z0-9_]+):`)
)

func yamlQuote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

func blankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

// hooksBlock finds the top-level `hooks:` mapping: the key line and the end of its children.
func hooksBlock(lines []string) (start, end int) {
	start = -1
	for i, l := range lines {
		if yamlHooksKey.MatchString(l) {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, -1
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if !blankOrComment(lines[i]) && indentOf(lines[i]) == 0 {
			end = i
			break
		}
	}
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" { // trailing blanks belong to nobody
		end--
	}
	return start, end
}

func installHermes(cfg *Config, path, exe string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text := removeHermesHooks(string(data))
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if yamlHooksNil.MatchString(l) {
			lines[i] = "hooks:"
		} else if strings.HasPrefix(l, "hooks:") && !yamlHooksKey.MatchString(l) {
			return fmt.Errorf("%s has a `hooks:` value agent-ping can't edit — add the hooks by hand, see README", path)
		}
	}

	start, end := hooksBlock(lines)
	indent := 2
	if start >= 0 {
		for i := start + 1; i < end; i++ {
			if blankOrComment(lines[i]) {
				continue
			}
			if indent = indentOf(lines[i]); indent == 0 {
				indent = 2
			}
			break
		}
		for i := start + 1; i < end; i++ {
			if m := yamlChildKey.FindStringSubmatch(lines[i]); m != nil && len(m[1]) == indent {
				for _, e := range hermesEvents {
					if m[2] == e[0] {
						return fmt.Errorf("%s already has `hooks: %s:` — add agent-ping's entry to it by hand, see README", path, e[0])
					}
				}
			}
		}
	}

	pad := strings.Repeat(" ", indent)
	var ours []string
	for _, e := range hermesEvents {
		ours = append(ours, pad+e[0]+":", pad+"  - command: "+yamlQuote(hookCmd(exe, e[1], false)))
	}
	if start >= 0 {
		lines = append(lines[:start+1], append(ours, lines[start+1:]...)...)
		text = strings.Join(lines, "\n")
	} else {
		text = strings.Join(lines, "\n")
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "hooks:\n" + strings.Join(ours, "\n") + "\n"
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	backup(path)
	if err := writeKeepMode(path, []byte(text)); err != nil {
		return err
	}
	return approveHermes(exe)
}

// removeHermesHooks drops our list items (and event keys left empty) from the hooks block.
// It works on Hermes' own rewrite of the file too, so it doesn't rely on marker comments.
func removeHermesHooks(text string) string {
	lines := strings.Split(text, "\n")
	start, end := hooksBlock(lines)
	if start < 0 {
		return text
	}
	var keep []string
	i := start + 1
	for i < end {
		m := yamlChildKey.FindStringSubmatch(lines[i])
		if m == nil || !isHermesEvent(m[2]) {
			keep = append(keep, lines[i])
			i++
			continue
		}
		// One event key: split its body into `- ` items and keep the ones that aren't ours.
		keyIndent := len(m[1])
		j := i + 1
		var items [][]string
		var cur []string
		for ; j < end && (blankOrComment(lines[j]) || indentOf(lines[j]) > keyIndent); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "- ") && cur != nil {
				items = append(items, cur)
				cur = nil
			}
			cur = append(cur, lines[j])
		}
		if cur != nil {
			items = append(items, cur)
		}
		var body []string
		dropped := false
		for _, it := range items {
			if isOurs(strings.Join(it, " ")) {
				dropped = true
				continue
			}
			body = append(body, it...)
		}
		if !dropped || !onlyBlank(body) {
			keep = append(keep, lines[i])
			keep = append(keep, body...)
		}
		i = j
	}
	out := append([]string{}, lines[:start]...)
	if !onlyBlank(keep) {
		out = append(append(out, lines[start]), keep...)
	}
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

func isHermesEvent(name string) bool {
	for _, e := range hermesEvents {
		if e[0] == name {
			return true
		}
	}
	return false
}

func onlyBlank(lines []string) bool {
	for _, l := range lines {
		if !blankOrComment(l) {
			return false
		}
	}
	return true
}

func uninstallHermes(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if text := removeHermesHooks(string(data)); text != string(data) {
		if err := writeKeepMode(path, []byte(text)); err != nil {
			return err
		}
	}
	return approveHermes("")
}

// approveHermes records consent for our hooks in Hermes' allowlist (installing them is the consent),
// so Hermes doesn't ask on first use and non-TTY runs don't skip them. exe "" only removes ours.
func approveHermes(exe string) error {
	path := hermesAllowlist()
	root, err := readJSON(path)
	if err != nil {
		return err
	}
	list, _ := root["approvals"].([]any)
	var keep []any
	for _, x := range list {
		if xm, ok := x.(map[string]any); ok && isOurs(fmt.Sprint(xm["command"])) {
			continue
		}
		keep = append(keep, x)
	}
	if exe != "" {
		now := time.Now().UTC().Format(time.RFC3339)
		for _, e := range hermesEvents {
			keep = append(keep, map[string]any{"event": e[0], "command": hookCmd(exe, e[1], false), "approved_at": now})
		}
	}
	if keep == nil {
		keep = []any{}
	}
	root["approvals"] = keep
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) && exe == "" {
		return nil
	}
	return writeJSON(path, root)
}
