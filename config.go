package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Channel struct {
	Type   string `json:"type"`             // ntfy | bark | pushover | telegram | webhook
	Server string `json:"server,omitempty"` // ntfy / bark server
	Topic  string `json:"topic,omitempty"`  // ntfy topic
	Token  string `json:"token,omitempty"`  // ntfy access token, pushover app token, telegram bot token
	Key    string `json:"key,omitempty"`    // bark device key, pushover user key
	ChatID string `json:"chat_id,omitempty"`
	URL    string `json:"url,omitempty"` // webhook
}

type Config struct {
	Lang         string    `json:"lang"`
	Channels     []Channel `json:"channels"`
	MinSeconds   int       `json:"min_seconds"`
	IdleSeconds  int       `json:"idle_seconds"`
	IncludeText  bool      `json:"include_text"`
	SkipApproval bool      `json:"skip_approval"`
	CodexChain   []string  `json:"codex_chain,omitempty"` // a notify program Codex had before us; we still call it
	Machine      string    `json:"machine,omitempty"`     // shown in every notification, to tell computers apart
	Off          []string  `json:"off,omitempty"`         // agents switched off with `agent-ping off`; their hooks stay but stay quiet
}

func configDir() string {
	if d := os.Getenv("AGENT_PING_HOME"); d != "" {
		return d
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = filepath.Join(homeDir(), ".config")
	}
	return filepath.Join(d, "agent-ping")
}

func configPath() string { return filepath.Join(configDir(), "config.json") }

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

func loadConfig() (*Config, error) {
	cfg := &Config{}
	data, err := os.ReadFile(configPath())
	if errors.Is(err, fs.ErrNotExist) {
		cfg.Lang = detectLang()
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}
	if cfg.Lang == "" {
		cfg.Lang = detectLang()
	}
	return cfg, nil
}

func saveConfig(cfg *Config) error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(configPath(), append(data, '\n'), 0o600) // holds tokens
}

func detectLang() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.ToLower(os.Getenv(k)); v != "" {
			if strings.HasPrefix(v, "zh") {
				return "zh"
			}
			break
		}
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
			s := strings.ToLower(string(out))
			if i := strings.Index(s, "\""); i >= 0 && strings.HasPrefix(s[i+1:], "zh") {
				return "zh"
			}
		}
	}
	return "en"
}

func stateDir() string {
	if d := os.Getenv("AGENT_PING_HOME"); d != "" {
		return filepath.Join(d, "state")
	}
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "agent-ping", "state")
}
