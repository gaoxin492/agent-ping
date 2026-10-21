# agent-ping

AI 编程助手跑完任务、或者需要你确认时，手机（和手表）会震一下。

支持 **Claude Code、Codex、Cursor、Gemini CLI、Hermes**，其他任何命令也可以用 `agent-ping run`。
手表上不用装任何东西：Apple Watch 和大多数手环本来就会同步手机通知。

**目录：** [安装](#安装) · [手机 App](#手机-app) · [支持的 Agent](#支持的-agent) · [设置](#设置) · [卸载](#卸载) · [隐私](#隐私)

## 安装

macOS / Linux：

```sh
curl -fsSL https://raw.githubusercontent.com/gaoxin492/agent-ping/main/install.sh | sh
```

Windows（PowerShell）：

```powershell
irm https://raw.githubusercontent.com/gaoxin492/agent-ping/main/install.ps1 | iex
```

安装过程会问你用哪个手机 App，发一条测试通知，然后自动给找到的所有 agent 加上 hook。
装完把已经开着的 agent 会话重启一下。

## 手机 App

| App | 手机 | 说明 |
|---|---|---|
| ntfy（默认） | iPhone、安卓 | 免费，不用注册，订阅安装时显示的主题名即可 |
| Bark | iPhone | 粘贴 App 里显示的链接 |
| Pushover | iPhone、安卓 | 一次性付费 |
| Telegram | 都行 | 用你自己的机器人 |
| Webhook | — | Slack、Discord、飞书，或你自己的服务器 |

随时切换：`agent-ping use bark https://api.day.app/你的KEY`

## 支持的 Agent

| Agent | 什么时候提醒 |
|---|---|
| Claude Code | 回复完成，或请求权限时 |
| Codex CLI | 一轮任务完成 |
| Cursor | agent 停下（或出错） |
| Gemini CLI | 回复完成，或请求权限时 |
| Hermes | 一轮任务完成，或要你批准命令时（CLI / TUI / 桌面版；Telegram 等网关里的对话不提醒） |
| 其他任何工具 | `agent-ping run -- 命令`，命令结束时提醒 |

`agent-ping install claude` 只给一个 agent 加 hook。`agent-ping status` 查看当前状态。

## 设置

```sh
agent-ping set min_seconds 30     # 短任务不提醒
agent-ping set idle 60            # 你在用电脑时不提醒
agent-ping set include_text true  # 通知里带上回复开头
agent-ping set approval false     # 请求权限时不提醒
agent-ping set lang en            # English
```

## 卸载

```sh
agent-ping uninstall
```

原来的 agent 配置文件会备份在旁边，文件名是 `*.agent-ping.bak`。

## 隐私

通知只包含 agent 名称、项目文件夹名和用时；回复内容默认**不发送**。
默认的 ntfy 服务器是公开的：知道主题名的人都能看到通知，所以请保留安装时生成的随机主题名（或自己部署 ntfy）。
