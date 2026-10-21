package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const exe = "/usr/local/bin/agent-ping"

func TestClaudeMergeKeepsUserHooks(t *testing.T) {
	root := map[string]any{}
	json.Unmarshal([]byte(`{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"say done"}]}]}}`), &root)
	for i := 0; i < 2; i++ { // installing twice must not duplicate
		for _, e := range claudeEvents {
			addNested(root, e[0], hookCmd(exe, e[1], true))
		}
	}
	stop := root["hooks"].(map[string]any)["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("want user hook + ours, got %v", stop)
	}
	for ev := range root["hooks"].(map[string]any) {
		removeNested(root, ev)
	}
	want := map[string]any{}
	json.Unmarshal([]byte(`{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"say done"}]}]}}`), &want)
	if !reflect.DeepEqual(root, want) {
		t.Fatalf("uninstall should restore original, got %v", root)
	}
}

func TestCursorFlat(t *testing.T) {
	root := map[string]any{}
	for _, e := range cursorEvents {
		addFlat(root, e[0], hookCmd(exe, e[1], false))
	}
	addFlat(root, "stop", hookCmd(exe, "cursor-stop", false))
	if n := len(root["hooks"].(map[string]any)["stop"].([]any)); n != 1 {
		t.Fatalf("dup stop hooks: %d", n)
	}
	removeFlat(root, "stop")
	removeFlat(root, "beforeSubmitPrompt")
	if _, ok := root["hooks"]; ok {
		t.Fatalf("hooks should be gone: %v", root)
	}
}

func TestCodexToml(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_PING_HOME", dir)
	path := filepath.Join(dir, "config.toml")
	orig := "model = \"gpt-5\"\nnotify = [\n  \"/bin/bash\", # shell\n  '/Users/me/notify.sh',\n]\n\n[tui]\nnotifications = true\n"
	os.WriteFile(path, []byte(orig), 0o644)
	cfg, _ := loadConfig()

	if err := installCodex(cfg, path, `C:\Users\me\agent-ping.exe`); err != nil {
		t.Fatal(err)
	}
	if err := installCodex(cfg, path, `C:\Users\me\agent-ping.exe`); err != nil { // twice
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if strings.Count(string(got), "notify =") != 1 || !strings.Contains(string(got), `"C:\\Users\\me\\agent-ping.exe", "hook", "codex"`) {
		t.Fatalf("bad install:\n%s", got)
	}
	if !reflect.DeepEqual(cfg.CodexChain, []string{"/bin/bash", "/Users/me/notify.sh"}) {
		t.Fatalf("chain not saved: %v", cfg.CodexChain)
	}
	if _, _, v := findNotify(string(got)); v[0] != `C:\Users\me\agent-ping.exe` {
		t.Fatalf("round trip: %v", v)
	}

	uninstallCodex(cfg, path)
	got, _ = os.ReadFile(path)
	if !strings.Contains(string(got), `notify = ["/bin/bash", "/Users/me/notify.sh"]`) || strings.Contains(string(got), "agent-ping") {
		t.Fatalf("bad uninstall:\n%s", got)
	}
}

func TestCodexTomlFresh(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_PING_HOME", dir)
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[profiles.x]\nnotify = \"not top level\"\n"), 0o644)
	cfg, _ := loadConfig()
	installCodex(cfg, path, exe)
	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), `notify = ["/usr/local/bin/agent-ping", "hook", "codex"]`) {
		t.Fatalf("should insert at top:\n%s", got)
	}
	uninstallCodex(cfg, path)
	got, _ = os.ReadFile(path)
	if string(got) != "[profiles.x]\nnotify = \"not top level\"\n" {
		t.Fatalf("should restore:\n%q", got)
	}
}

func TestParseBark(t *testing.T) {
	c, err := parseBark("https://api.day.app/AbCdEf123/这里改成你自己的推送内容")
	if err != nil || c.Key != "AbCdEf123" || c.Server != "https://api.day.app" {
		t.Fatalf("%v %v", c, err)
	}
}

func TestHermesYaml(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_PING_HOME", dir)
	t.Setenv("HERMES_HOME", dir)
	path := filepath.Join(dir, "config.yaml")
	cfg, _ := loadConfig()
	cases := map[string]string{
		"no hooks":     "model:\n  default: opus\n# trailing comment\n",
		"empty file":   "",
		"user hooks":   "model: x\nhooks:\n    post_tool_call:\n        - matcher: \"write_file\"\n          command: \"~/fmt.sh\"\n    outbound:\n        - url: https://x\nhooks_auto_accept: false\n",
		"hooks at end": "a: 1\n\nhooks:\n  post_llm_call:\n    - command: \"log.sh\"\n",
	}
	for name, orig := range cases {
		os.WriteFile(path, []byte(orig), 0o600)
		os.Remove(path + ".agent-ping.bak")
		for i := 0; i < 2; i++ { // installing twice must not duplicate
			if err := installHermes(cfg, path, "/Users/me/My Tools/agent-ping"); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		got, _ := os.ReadFile(path)
		for _, e := range hermesEvents {
			want := e[0] + ":\n"
			if strings.Count(string(got), want) != 1 || strings.Count(string(got), "hook "+e[1]) != 1 {
				t.Fatalf("%s: want one %s entry:\n%s", name, e[0], got)
			}
		}
		if !strings.Contains(string(got), `- command: "\"/Users/me/My Tools/agent-ping\" hook hermes-stop"`) {
			t.Fatalf("%s: bad quoting:\n%s", name, got)
		}
		if strings.Count(string(got), "hooks:") != 1 {
			t.Fatalf("%s: duplicate hooks key:\n%s", name, got)
		}
		if name == "user hooks" && !strings.Contains(string(got), "hooks:\n    pre_llm_call:\n      - command:") {
			t.Fatalf("should match the block's indent:\n%s", got)
		}
		if err := uninstallHermes(path); err != nil {
			t.Fatal(err)
		}
		got, _ = os.ReadFile(path)
		if string(got) != orig && !(orig == "" && string(got) == "") {
			t.Fatalf("%s: uninstall should restore:\n%q\nvs\n%q", name, got, orig)
		}
	}

	allow, _ := readJSON(hermesAllowlist())
	if list, _ := allow["approvals"].([]any); len(list) != 0 {
		t.Fatalf("allowlist should be clean after uninstall: %v", allow)
	}
}

func TestHermesAllowlistKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERMES_HOME", dir)
	os.WriteFile(hermesAllowlist(), []byte(`{"approvals":[{"event":"post_tool_call","command":"~/fmt.sh"}]}`), 0o600)
	approveHermes(exe)
	approveHermes(exe)
	root, _ := readJSON(hermesAllowlist())
	list := root["approvals"].([]any)
	if len(list) != 1+len(hermesEvents) {
		t.Fatalf("want user entry + ours once: %v", list)
	}
	approveHermes("")
	root, _ = readJSON(hermesAllowlist())
	if list := root["approvals"].([]any); len(list) != 1 || list[0].(map[string]any)["command"] != "~/fmt.sh" {
		t.Fatalf("should keep only the user's entry: %v", list)
	}
}

func TestHermesYamlConflict(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERMES_HOME", dir)
	path := filepath.Join(dir, "config.yaml")
	orig := "hooks:\n  on_session_end:\n    - command: \"mine.sh\"\n"
	os.WriteFile(path, []byte(orig), 0o600)
	cfg, _ := loadConfig()
	if err := installHermes(cfg, path, exe); err == nil {
		t.Fatal("should refuse to touch an event the user already hooks")
	}
	if got, _ := os.ReadFile(path); string(got) != orig {
		t.Fatalf("file must be untouched:\n%s", got)
	}
	os.WriteFile(path, []byte("hooks: {}\n"), 0o600)
	if err := installHermes(cfg, path, exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !strings.HasPrefix(string(got), "hooks:\n  pre_llm_call:\n") {
		t.Fatalf("hooks: {} should become a block:\n%s", got)
	}
}

func TestHermesPayload(t *testing.T) {
	p := map[string]any{}
	json.Unmarshal([]byte(`{"hook_event_name":"on_session_end","session_id":"s1","cwd":"/w/proj","extra":{"completed":false,"failed":true,"interrupted":false,"platform":"cli"}}`), &p)
	if !hermesPayload(p) || p["status"] != "error" || sessionKey(p) != "s1" || projectName(p) != "proj" {
		t.Fatalf("cli turn: %v", p)
	}
	p = map[string]any{"extra": map[string]any{"platform": "cli", "interrupted": true}}
	if !hermesPayload(p) || p["status"] != "aborted" {
		t.Fatalf("interrupted: %v", p)
	}
	if hermesPayload(map[string]any{"extra": map[string]any{"platform": "telegram"}}) {
		t.Fatal("gateway turns should be skipped")
	}
	if hermesPayload(map[string]any{"extra": map[string]any{"surface": "smart"}}) {
		t.Fatal("smart approvals should be skipped")
	}
	p = map[string]any{"extra": map[string]any{"surface": "cli", "description": "delete files", "command": "rm -rf x"}}
	if !hermesPayload(p) || p["message"] != "delete files" {
		t.Fatalf("approval message: %v", p)
	}
}
