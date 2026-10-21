package main

import (
	"fmt"
	"time"
)

var texts = map[string][2]string{ // {en, zh}
	"done":         {"✅ %s finished", "✅ %s 完成了"},
	"approve":      {"⏳ %s needs your OK", "⏳ %s 需要你确认"},
	"error":        {"❌ %s hit an error", "❌ %s 出错了"},
	"project":      {"Project: %s", "项目：%s"},
	"took":         {"Time: %s", "用时：%s"},
	"cmd_done":     {"✅ Command finished", "✅ 命令跑完了"},
	"cmd_fail":     {"❌ Command failed (exit %d)", "❌ 命令失败（退出码 %d）"},
	"test_title":   {"🔔 agent-ping test", "🔔 agent-ping 测试"},
	"test_body":    {"If your watch buzzed, you're all set.", "手表震了就说明配置成功。"},
	"no_channel":   {"no phone app set up yet — run: agent-ping setup", "还没设置手机推送 —— 请运行：agent-ping setup"},
	"sent":         {"Sent. Check your phone / watch.", "已发送，看看手机 / 手表。"},
	"hooked":       {"✓ %s: hooks added → %s", "✓ %s：已添加 hook → %s"},
	"unhooked":     {"✓ %s: hooks removed → %s", "✓ %s：已移除 hook → %s"},
	"notfound":     {"- %s: not found, skipped", "- %s：没找到，跳过"},
	"failed":       {"✗ %s: %v", "✗ %s：%v"},
	"restart":      {"Restart any open agent sessions so they load the new hooks.", "已经开着的 agent 会话需要重启一下才会生效。"},
	"ask":          {"❓ %s is asking you something", "❓ %s 在问你问题"},
	"machine":      {"Computer: %s", "电脑：%s"},
	"switched_on":  {"🔔 %s: on", "🔔 %s：已开启提醒"},
	"switched_off": {"🔕 %s: off (hooks kept; agent-ping on to resume)", "🔕 %s：已关闭提醒（hook 保留，agent-ping on 即可恢复）"},
	"codex_chain":  {"  (Codex already had a notify program; agent-ping will keep calling it.)", "  （Codex 原来已有 notify 程序，agent-ping 会继续调用它。）"},
}

func T(cfg *Config, key string, args ...any) string {
	t, ok := texts[key]
	if !ok {
		return key
	}
	s := t[0]
	if cfg != nil && cfg.Lang == "zh" {
		s = t[1]
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

func fmtDuration(cfg *Config, d time.Duration) string {
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	zh := cfg != nil && cfg.Lang == "zh"
	switch {
	case h > 0 && zh:
		return fmt.Sprintf("%d小时%d分", h, m)
	case h > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case m > 0 && zh:
		return fmt.Sprintf("%d分%d秒", m, s)
	case m > 0:
		return fmt.Sprintf("%dm%ds", m, s)
	case zh:
		return fmt.Sprintf("%d秒", s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

var agentNames = map[string]string{
	"claude": "Claude Code",
	"codex":  "Codex",
	"cursor": "Cursor",
	"gemini": "Gemini CLI",
	"hermes": "Hermes",
}
