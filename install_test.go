package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
			addNested(root, e[0], e[2], hookCmd(exe, e[1], true))
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

func TestSwitchOnOff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_PING_HOME", dir)
	t.Setenv("HERMES_HOME", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("CODEX_HOME", dir)
	t.Setenv("HOME", dir)
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/agent-ping hook claude-stop"}]}]}}`), 0o600)
	if err := cmdSwitch(false, []string{"Claude"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := loadConfig()
	if !cfg.isOff("claude") || cfg.isOff("hermes") {
		t.Fatalf("off: %v", cfg.Off)
	}
	cmdSwitch(false, []string{"claude"}) // twice must not duplicate
	if cfg, _ = loadConfig(); len(cfg.Off) != 1 {
		t.Fatalf("dup: %v", cfg.Off)
	}
	if err := cmdSwitch(true, nil); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = loadConfig(); len(cfg.Off) != 0 {
		t.Fatalf("on with no args should clear all: %v", cfg.Off)
	}
	if err := cmdSwitch(false, []string{"chatgpt"}); err == nil {
		t.Fatal("unknown agent should fail")
	}
}

// captured starts a local webhook and returns a func listing what was pushed to it.
func captured(t *testing.T, cfg *Config) func() []string {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		got = append(got, fmt.Sprint(m["title"])+" | "+fmt.Sprint(m["message"]))
	}))
	t.Cleanup(srv.Close)
	cfg.Lang = "en"
	cfg.Channels = []Channel{{Type: "webhook", URL: srv.URL}}
	return func() []string { return got }
}

func TestAskThenPermissionNoticeIsOnePing(t *testing.T) {
	t.Setenv("AGENT_PING_HOME", t.TempDir())
	cfg := &Config{}
	pushed := captured(t, cfg)
	q := map[string]any{"session_id": "s9", "cwd": "/w/x", "tool_input": map[string]any{}}
	n := map[string]any{"session_id": "s9", "cwd": "/w/x", "notification_type": "permission_prompt", "message": "Claude needs your permission"}
	askingYou(cfg, "claude", q)
	approval(cfg, "claude", n)
	approval(cfg, "claude", n) // a later real permission prompt still pings
	if len(pushed()) != 2 || !strings.HasPrefix(pushed()[0], "❓") || !strings.HasPrefix(pushed()[1], "⏳") {
		t.Fatalf("got %q", pushed())
	}
}

func TestApprovalAndAsk(t *testing.T) {
	t.Setenv("AGENT_PING_HOME", t.TempDir())
	cfg := &Config{}
	pushed := captured(t, cfg)
	p := func(s string) map[string]any { m := map[string]any{}; json.Unmarshal([]byte(s), &m); return m }

	approval(cfg, "claude", p(`{"notification_type":"auth_success","message":"Logged in","cwd":"/w/a"}`))
	approval(cfg, "claude", p(`{"notification_type":"idle_prompt","message":"Claude is waiting for your input","cwd":"/w/a"}`))
	if len(pushed()) != 0 {
		t.Fatalf("auth_success / idle_prompt must not ping: %v", pushed())
	}
	approval(cfg, "claude", p(`{"notification_type":"permission_prompt","message":"Claude needs your permission to use Bash","cwd":"/w/a"}`))
	approval(cfg, "claude", p(`{"notification_type":"elicitation_dialog","message":"Fill in the form","cwd":"/w/a"}`))
	approval(cfg, "codex", p(`{"hook_event_name":"PermissionRequest","cwd":"/w/b","tool_name":"Bash","tool_input":{"command":"rm -rf x","description":"Delete build output"}}`))
	askingYou(cfg, "hermes", p(`{"cwd":"/w/c","tool_input":{"question":"Which DB?"}}`))
	cfg.IncludeText, cfg.Machine = true, "Work PC"
	askingYou(cfg, "claude", p(`{"cwd":"/w/d","tool_input":{"questions":[{"question":"Pick a color","options":[]}]}}`))

	want := []string{
		"⏳ Claude Code needs your OK | Project: a\nClaude needs your permission to use Bash",
		"❓ Claude Code is asking you something | Project: a\nFill in the form",
		"⏳ Codex needs your OK | Project: b\nDelete build output",
		"❓ Hermes is asking you something | Project: c",
		"❓ Claude Code is asking you something | Project: d\nPick a color\nComputer: Work PC",
	}
	if !reflect.DeepEqual(pushed(), want) {
		t.Fatalf("got:\n%q\nwant:\n%q", pushed(), want)
	}
	if strings.Contains(strings.Join(pushed(), ""), "rm -rf") {
		t.Fatal("the raw command must never be pushed")
	}
}

func TestCodexHooksJson(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	cfg := &Config{}
	installCodexHooks(cfg, exe)
	installCodexHooks(cfg, exe)
	root, _ := readJSON(codexHooksFile())
	h := root["hooks"].(map[string]any)
	if len(h) != len(codexHookEvents) || len(h["PreToolUse"].([]any)) != 1 {
		t.Fatalf("bad hooks.json: %v", h)
	}
	if m := h["PreToolUse"].([]any)[0].(map[string]any)["matcher"]; m != "^request_user_input$" {
		t.Fatalf("matcher: %v", m)
	}
	uninstallCodexHooks()
	if _, err := os.Stat(codexHooksFile()); err == nil {
		t.Fatal("hooks.json with only our hooks should be removed")
	}
	os.WriteFile(codexHooksFile(), []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"say hi"}]}]}}`), 0o600)
	installCodexHooks(cfg, exe)
	uninstallCodexHooks()
	if got, _ := os.ReadFile(codexHooksFile()); !strings.Contains(string(got), "say hi") || strings.Contains(string(got), "agent-ping") {
		t.Fatalf("user hooks must survive: %s", got)
	}
}

func TestCodexTrustHash(t *testing.T) {
	// Hashes Codex itself wrote after "Trust All" for these exact hooks.
	cmd := "/Users/xingao/.local/bin/agent-ping hook "
	cases := [][4]string{
		{"UserPromptSubmit", "", "codex-start", "sha256:ced22e91a5ff027bd246e42c7453738a18bbe949bc401625cee931a4632ef7e2"},
		{"PermissionRequest", "", "codex-notify", "sha256:b59d0a52ebd2ba0cf61b9c32c36de3e2448ce87051f157ed4d3151507015df0c"},
		{"PreToolUse", "^request_user_input$", "codex-ask", "sha256:c486949902fff744e8861278d5f8cce3448ec6933f072c919cbcc9b4fe031a92"},
	}
	for _, c := range cases {
		if got := codexHookHash(c[0], c[1], cmd+c[2]); got != c[3] {
			t.Errorf("%s: %s", c[0], got)
		}
	}
}

func TestCodexTrustRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	path := filepath.Join(dir, "config.toml")
	orig := "model = \"x\"\n\n[hooks.state.\"/elsewhere/hooks.json:stop:0:0\"]\ntrusted_hash = \"sha256:user\"\n\n[tui]\nnotifications = true\n"
	os.WriteFile(path, []byte(orig), 0o600)
	cfg := &Config{}
	installCodexHooks(cfg, exe)
	installCodexHooks(cfg, exe) // twice: no duplicate records
	got, _ := os.ReadFile(path)
	if n := strings.Count(string(got), "[hooks.state."); n != 1+len(codexHookEvents) {
		t.Fatalf("want user record + %d of ours, got %d:\n%s", len(codexHookEvents), n, got)
	}
	key := `[hooks.state."` + filepath.Join(dir, "hooks.json") + `:pre_tool_use:0:0"]`
	if !strings.Contains(string(got), key) {
		t.Fatalf("missing %s:\n%s", key, got)
	}
	uninstallCodexHooks()
	if got, _ := os.ReadFile(path); string(got) != orig {
		t.Fatalf("uninstall should restore:\n%q\nvs\n%q", got, orig)
	}
}

func TestCodexTurnDedupe(t *testing.T) {
	t.Setenv("AGENT_PING_HOME", t.TempDir())
	p := map[string]any{"turn-id": "t1"}
	if !firstForTurn("codex", p) || firstForTurn("codex", p) {
		t.Fatal("second report of the same turn should be dropped")
	}
	if !firstForTurn("codex", map[string]any{}) {
		t.Fatal("no turn id: always report")
	}
}
