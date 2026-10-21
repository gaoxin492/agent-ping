# agent-ping

AI 编程助手跑完任务、或者需要你确认时，手机（和手表）会震一下。

支持 **Claude Code、Codex、Cursor、Gemini CLI、Hermes**，其他任何命令也可以用 `agent-ping run`。
手表上不用装任何东西：Apple Watch 和大多数手环本来就会同步手机通知。

**目录：** [安装](#安装) · [多台电脑](#多台电脑共用一个手机) · [手机 App](#手机-app) · [支持的 Agent](#支持的-agent) · [设置](#设置) · [卸载](#卸载) · [隐私](#隐私)

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

## 多台电脑共用一个手机

每台电脑都装一次 agent-ping，然后都用**同一个主题**（ntfy）或**同一个 Key**（Bark）。手机上只需订阅一次。

1. 在已经装好的电脑上查主题名：
   ```sh
   agent-ping status        # channel: ntfy https://ntfy.sh/你的主题名
   ```
2. 在新电脑上安装。问到「主题名」时，**别直接回车**，粘贴上面那个主题名。
   装过了也没关系，直接改：
   ```sh
   agent-ping use ntfy 你的主题名
   agent-ping use bark https://api.day.app/你的KEY   # 用 Bark 的话
   agent-ping test
   ```

通知里有项目文件夹名；再用 `agent-ping set machine 名字` 给每台电脑起个名，就能分清是哪台电脑、哪个项目。

### Windows + WSL

agent-ping 只管**和它装在同一边**的 agent：

| 你的 agent 跑在哪 | 在哪装 | 安装命令 |
|---|---|---|
| WSL 终端里（Ubuntu 等） | WSL 里 | `curl -fsSL https://raw.githubusercontent.com/gaoxin492/agent-ping/main/install.sh \| sh` |
| Windows 本身（PowerShell、Cursor、Claude 桌面版） | PowerShell 里 | `irm https://raw.githubusercontent.com/gaoxin492/agent-ping/main/install.ps1 \| iex` |

两边都用就**两边都装**，各自有一份配置。按上面的方法填同一个主题名，就能推到同一个手机。

- WSL 里一般读不到键盘空闲时间，所以 `idle` 设置在 WSL 里不起作用，任务完成就会提醒。
- WSL 里的 agent 配置在 Linux 家目录（如 `~/.claude`），和 Windows 的 `C:\Users\你\.claude` 是两份，互不影响。

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

三种提醒：**✅ 完成了**、**⏳ 需要你确认**（批准命令或权限）、**❓ 在问你问题**（让你选择或回答）。

| Agent | ✅ 完成 | ⏳ 需要你确认 | ❓ 在问你问题 |
|---|---|---|---|
| Claude Code | ✅ | ✅ | ✅ 选项提问（AskUserQuestion）、MCP 表单 |
| Codex | ✅ | 见下方说明 | request_user_input（已接上，未实测） |
| Cursor | ✅（含出错） | — | — |
| Gemini CLI | ✅ | ✅ | — |
| Hermes | ✅ | ✅ | ✅ clarify 提问 |
| 其他任何工具 | `agent-ping run -- 命令`，命令结束时提醒 | | |

**在哪里用都行：** 同一份 hook 对这些入口都有效，下面这些都实测过——
终端里的 Claude Code / Codex / Hermes、**Claude 桌面版**、**VS Code 的 Claude Code 插件**、**VS Code 的 Codex 插件**、**ChatGPT 桌面版里的 Codex**。
网页版聊天（claude.ai、chatgpt.com）和 ChatGPT 普通聊天没有开放 hook，无法提醒。

**Codex 说明：** 「完成」用 Codex 的 `notify`，所有版本、所有入口都会调用。
开始时间、批准、提问用 Codex 新的 hook（`~/.codex/hooks.json`）。Codex 要求先信任 hook 才会运行，agent-ping 安装时会自动写好信任记录，不用你在 `/hooks` 里点。
「需要你确认」依赖 Codex 在请求批准时调用 `PermissionRequest`：在 Codex 0.162（alpha）上实测还没有触发，更新到更新的版本后会自动生效。

**Hermes 说明：** 只给默认配置（`~/.hermes`）加 hook；如果你用 `hermes profile` 建了多套配置，其他 profile 不提醒。Telegram 等网关、定时任务、子 agent 里的对话也不提醒。

`agent-ping install claude` 只给一个 agent 加 hook。`agent-ping status` 查看当前状态。

### 随时开关某个 agent 的提醒

```sh
agent-ping off codex           # 关掉 Codex 的提醒
agent-ping on claude hermes    # 打开 Claude Code 和 Hermes（还没加 hook 的会顺便加上）
agent-ping off                 # 全部暂停
agent-ping on                  # 全部恢复
agent-ping status              # 看哪些开着、哪些关着
```

`off` 只是让 agent-ping 不发通知，hook 留着，所以立刻生效，不用重启 agent。

## 设置

```sh
agent-ping set min_seconds 30     # 短任务不提醒
agent-ping set idle 60            # 你在用电脑时不提醒
agent-ping set include_text true  # 通知里带上回复开头
agent-ping set approval false     # 请求权限、提问时不提醒
agent-ping set machine 公司电脑   # 通知里写上电脑名（多台电脑时分得清）；"-" 去掉
agent-ping set lang en            # 通知改成英文（安装时第一个问题就是选语言）
```

通知里的「项目」是 agent 当时所在的文件夹名，例如在 `~/code/my-app` 里跑就显示「项目：my-app」。

## 卸载

```sh
agent-ping uninstall
```

原来的 agent 配置文件会备份在旁边，文件名是 `*.agent-ping.bak`。

## 隐私

通知只包含 agent 名称、项目文件夹名和用时；回复内容默认**不发送**。
默认的 ntfy 服务器是公开的：知道主题名的人都能看到通知，所以请保留安装时生成的随机主题名（或自己部署 ntfy）。
