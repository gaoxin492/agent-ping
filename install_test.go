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
