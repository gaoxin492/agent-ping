package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func cmdTest() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if err := sendAll(cfg, Message{Title: T(cfg, "test_title"), Body: T(cfg, "test_body")}); err != nil {
		return err
	}
	fmt.Println(T(cfg, "sent"))
	return nil
}

func cmdSend(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: agent-ping send <title> [body]")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	return sendAll(cfg, Message{Title: args[0], Body: strings.Join(args[1:], " ")})
}

func cmdRun(args []string) int {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: agent-ping run -- <command> [args...]")
		return 2
	}
	cfg, _ := loadConfig()
	start := time.Now()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = 1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			fmt.Fprintln(os.Stderr, "agent-ping:", err)
		}
	}
	dur := time.Since(start)
	if cfg.MinSeconds > 0 && dur < time.Duration(cfg.MinSeconds)*time.Second || !userAway(cfg) {
		return code
	}
	title := T(cfg, "cmd_done")
	if code != 0 {
		title = T(cfg, "cmd_fail", code)
	}
	body := snippet(strings.Join(args, " "), 100) + "\n" + T(cfg, "took", fmtDuration(cfg, dur))
	if err := sendAll(cfg, Message{Title: title, Body: body}); err != nil {
		fmt.Fprintln(os.Stderr, "agent-ping:", err)
	}
	return code
}

func cmdStatus() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	fmt.Println("config:", configPath())
	if len(cfg.Channels) == 0 {
		fmt.Println("channel: (none) — run: agent-ping setup")
	}
	for _, c := range cfg.Channels {
		desc := c.Type
		switch c.Type {
		case "ntfy":
			desc += " " + or(c.Server, "https://ntfy.sh") + "/" + c.Topic
		case "bark":
			desc += " " + or(c.Server, "https://api.day.app") + "/" + mask(c.Key)
		case "pushover":
			desc += " user " + mask(c.Key)
		case "telegram":
			desc += " chat " + c.ChatID
		case "webhook":
			desc += " " + c.URL
		}
		fmt.Println("channel:", desc)
	}
	fmt.Printf("lang: %s  min_seconds: %d  idle: %d  include_text: %v  approval: %v\n",
		cfg.Lang, cfg.MinSeconds, cfg.IdleSeconds, cfg.IncludeText, !cfg.SkipApproval)
	h := hookedAgents()
	if len(h) == 0 {
		fmt.Println("hooked: (none) — run: agent-ping install")
	} else {
		fmt.Println("hooked:", strings.Join(h, ", "))
	}
	return nil
}

func mask(s string) string {
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "…" + s[len(s)-3:]
}

// ---- use / set ----

func channelFrom(args []string) (Channel, error) {
	if len(args) == 0 {
		return Channel{}, errors.New("which app? ntfy | bark | pushover | telegram | webhook")
	}
	need := func(n int, hint string) error {
		if len(args)-1 < n {
			return errors.New("usage: agent-ping use " + args[0] + " " + hint)
		}
		return nil
	}
	switch args[0] {
	case "ntfy":
		c := Channel{Type: "ntfy", Topic: randomTopic()}
		if len(args) > 1 {
			c.Topic = args[1]
		}
		if len(args) > 2 {
			c.Server = args[2]
		}
		return c, nil
	case "bark":
		if err := need(1, "<key-or-url>"); err != nil {
			return Channel{}, err
		}
		return parseBark(args[1])
	case "pushover":
		if err := need(2, "<app-token> <user-key>"); err != nil {
			return Channel{}, err
		}
		return Channel{Type: "pushover", Token: args[1], Key: args[2]}, nil
	case "telegram":
		if err := need(2, "<bot-token> <chat-id>"); err != nil {
			return Channel{}, err
		}
		return Channel{Type: "telegram", Token: args[1], ChatID: args[2]}, nil
	case "webhook":
		if err := need(1, "<url>"); err != nil {
			return Channel{}, err
		}
		return Channel{Type: "webhook", URL: args[1]}, nil
	}
	return Channel{}, fmt.Errorf("unknown app %q (ntfy | bark | pushover | telegram | webhook)", args[0])
}

// parseBark accepts the URL the Bark app shows (https://api.day.app/KEY/...) or a bare key.
func parseBark(s string) (Channel, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		return Channel{Type: "bark", Key: strings.Trim(s, "/")}, nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return Channel{}, err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if parts[0] == "" {
		return Channel{}, errors.New("no key found in that Bark URL")
	}
	return Channel{Type: "bark", Server: u.Scheme + "://" + u.Host, Key: parts[0]}, nil
}

func randomTopic() string {
	b := make([]byte, 10)
	rand.Read(b)
	return "agent-ping-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

func cmdUse(args []string) error {
	c, err := channelFrom(args)
	if err != nil {
		return err
	}
	cfg, _ := loadConfig()
	cfg.Channels = []Channel{c}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if c.Type == "ntfy" {
		fmt.Println("ntfy topic:", c.Topic)
	}
	return cmdTest()
}

func cmdSet(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: agent-ping set <min_seconds|idle|include_text|approval|lang> <value>")
	}
	cfg, _ := loadConfig()
	k, v := args[0], args[1]
	b, berr := strconv.ParseBool(v)
	n, nerr := strconv.Atoi(v)
	switch k {
	case "min_seconds":
		if nerr != nil {
			return nerr
		}
		cfg.MinSeconds = n
	case "idle":
		if nerr != nil {
			return nerr
		}
		cfg.IdleSeconds = n
	case "include_text":
		if berr != nil {
			return berr
		}
		cfg.IncludeText = b
	case "approval":
		if berr != nil {
			return berr
		}
		cfg.SkipApproval = !b
	case "lang":
		if v != "en" && v != "zh" {
			return errors.New("lang must be en or zh")
		}
		cfg.Lang = v
	default:
		return fmt.Errorf("unknown setting %q", k)
	}
	return saveConfig(cfg)
}

// ---- interactive setup ----

var in = bufio.NewReader(os.Stdin)

func ask(prompt, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", prompt, def)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	line, _ := in.ReadString('\n')
	if line = strings.TrimSpace(line); line == "" {
		return def
	}
	return line
}

func cmdSetup() error {
	cfg, _ := loadConfig()
	def := "1"
	if cfg.Lang == "zh" {
		def = "2"
	}
	if ask("Language / 语言  1) English  2) 中文", def) == "2" {
		cfg.Lang = "zh"
	} else {
		cfg.Lang = "en"
	}
	zh := cfg.Lang == "zh"
	L := func(en, z string) string {
		if zh {
			return z
		}
		return en
	}

	fmt.Println()
	fmt.Println(L("Which phone app should receive the notifications?", "用哪个手机 App 接收通知？"))
	fmt.Println(L("  1) ntfy      — iPhone + Android, free, no account (recommended)", "  1) ntfy      —— iPhone + 安卓，免费，不用注册（推荐）"))
	fmt.Println(L("  2) Bark      — iPhone only, simplest", "  2) Bark      —— 仅 iPhone，最简单"))
	fmt.Println(L("  3) Pushover  — iPhone + Android, one-time purchase", "  3) Pushover  —— iPhone + 安卓，一次性付费"))
	fmt.Println(L("  4) Telegram  — via a bot", "  4) Telegram  —— 通过机器人"))
	fmt.Println(L("  5) Webhook   — Slack / Discord / Feishu / anything", "  5) Webhook   —— Slack / Discord / 飞书 / 任意地址"))
	var c Channel
	var err error
	switch ask(L("Pick", "选择"), "1") {
	case "2":
		fmt.Println(L("Install “Bark” from the App Store, open it, and copy the example URL it shows (https://api.day.app/…).",
			"在 App Store 安装 “Bark”，打开后复制它显示的示例链接（https://api.day.app/…）。"))
		c, err = parseBark(ask(L("Paste the URL", "粘贴链接"), ""))
	case "3":
		fmt.Println(L("Get your User Key on pushover.net, and create an Application there for an API Token.",
			"在 pushover.net 获取 User Key，并在那里创建一个 Application 拿到 API Token。"))
		c = Channel{Type: "pushover", Key: ask("User Key", ""), Token: ask("API Token", "")}
	case "4":
		c, err = setupTelegram(L)
	case "5":
		c = Channel{Type: "webhook", URL: ask("Webhook URL", "")}
	default:
		c = Channel{Type: "ntfy", Server: "https://ntfy.sh", Topic: randomTopic()}
		c.Server = ask(L("ntfy server (just press Enter unless you run your own)", "ntfy 服务器（没自建服务器就直接回车）"), c.Server)
		c.Topic = ask(L("Topic (press Enter to keep this random one)", "主题名（直接回车用这个随机名，越难猜越安全）"), c.Topic)
		fmt.Println()
		fmt.Println(L("On your phone: install the “ntfy” app → tap + → subscribe to this topic:",
			"在手机上：安装 “ntfy” App → 点 + → 订阅这个主题："))
		fmt.Println("    " + c.Topic)
		if c.Server != "https://ntfy.sh" {
			fmt.Println(L("    (turn on “Use another server” and enter ", "    （打开“使用其他服务器”，填入 ") + c.Server + L(")", "）"))
		}
		ask(L("Press Enter when done", "订阅好后按回车"), "")
	}
	if err != nil {
		return err
	}
	cfg.Channels = []Channel{c}
	if err := saveConfig(cfg); err != nil {
		return err
	}

	for {
		if err := cmdTest(); err != nil {
			fmt.Println("✗", err)
		}
		if strings.HasPrefix(strings.ToLower(ask(L("Did your phone get it? (y = yes, r = resend)", "手机收到了吗？（y = 收到，r = 重发）"), "y")), "r") {
			continue
		}
		break
	}

	fmt.Println()
	n, _ := strconv.Atoi(ask(L("Only notify when a task took at least N seconds (0 = always)", "任务至少跑了几秒才提醒（0 = 每次都提醒）"), strconv.Itoa(cfg.MinSeconds)))
	cfg.MinSeconds = n
	idleDef := "n"
	if cfg.IdleSeconds > 0 {
		idleDef = "y"
	}
	if strings.HasPrefix(strings.ToLower(ask(L("Stay quiet while you're using the computer? (y/n)", "你正在用电脑时不提醒？（y/n）"), idleDef)), "y") {
		cfg.IdleSeconds = 60
	} else {
		cfg.IdleSeconds = 0
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	fmt.Println()
	return cmdInstall(nil)
}

func setupTelegram(L func(string, string) string) (Channel, error) {
	fmt.Println(L("Create a bot with @BotFather in Telegram and paste its token.", "在 Telegram 里找 @BotFather 创建机器人，粘贴它给的 token。"))
	token := ask("Bot token", "")
	ask(L("Now send any message to your bot, then press Enter", "现在给你的机器人随便发一条消息，然后按回车"), "")
	resp, err := httpClient.Get("https://api.telegram.org/bot" + token + "/getUpdates")
	chat := ""
	if err == nil {
		var r struct {
			Result []struct {
				Message struct {
					Chat struct {
						ID int64 `json:"id"`
					} `json:"chat"`
				} `json:"message"`
			} `json:"result"`
		}
		json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()
		if n := len(r.Result); n > 0 && r.Result[n-1].Message.Chat.ID != 0 {
			chat = strconv.FormatInt(r.Result[n-1].Message.Chat.ID, 10)
		}
	}
	chat = ask("Chat ID", chat)
	if chat == "" {
		return Channel{}, errors.New("no chat id")
	}
	return Channel{Type: "telegram", Token: token, ChatID: chat}, nil
}
