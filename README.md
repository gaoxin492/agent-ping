# agent-ping

Your phone (and watch) buzzes when an AI coding agent finishes or needs your OK.

Works with **Claude Code, Codex, Cursor, Gemini CLI** — and any command via `agent-ping run`.
Nothing to install on the watch: Apple Watch and most fitness bands already mirror phone notifications.

[中文说明](README.zh-CN.md)

**Contents:** [Install](#install) · [Phone apps](#phone-apps) · [Agents](#agents) · [Settings](#settings) · [Uninstall](#uninstall) · [Privacy](#privacy)

## Install

macOS / Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/gaoxin492/agent-ping/main/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/gaoxin492/agent-ping/main/install.ps1 | iex
```

The setup asks which phone app to use, sends a test, then hooks up every agent it finds.
Restart open agent sessions afterwards.

## Phone apps

| App | Phones | Notes |
|---|---|---|
| ntfy (default) | iPhone, Android | Free, no account. Subscribe to the topic setup shows you. |
| Bark | iPhone | Paste the URL the app shows. |
| Pushover | iPhone, Android | One-time purchase. |
| Telegram | any | Via your own bot. |
| Webhook | — | Slack, Discord, Feishu, or your own server. |

Switch anytime: `agent-ping use bark https://api.day.app/YOURKEY`

## Agents

| Agent | You get a ping when… |
|---|---|
| Claude Code | a reply is done, or it asks for permission |
| Codex CLI | a turn is done |
| Cursor | the agent stops (or errors) |
| Gemini CLI | a reply is done, or it asks for permission |
| Anything else | `agent-ping run -- <command>` pings when the command ends |

`agent-ping install claude` hooks just one agent. `agent-ping status` shows what's hooked.

## Settings

```sh
agent-ping set min_seconds 30     # skip quick tasks
agent-ping set idle 60            # stay quiet while you're at the keyboard
agent-ping set include_text true  # add the start of the agent's reply
agent-ping set approval false     # no pings for permission requests
agent-ping set lang zh            # 中文通知
```

## Uninstall

```sh
agent-ping uninstall
```

Your original agent config files are backed up next to them as `*.agent-ping.bak`.

## Privacy

Notifications contain the agent name, project folder name and run time. The agent's reply text is **off** by default.
The default ntfy server is public: anyone who knows your topic name can read it, so keep the random name setup picks
(or self-host ntfy).
