// agent-ping: buzz your phone / watch when an AI coding agent finishes.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func usage() {
	fmt.Print(`agent-ping ` + version + ` — get a phone/watch notification when your AI agent is done.

Usage:
  agent-ping setup                    Guided setup (pick a phone app, test it, install hooks)
  agent-ping install [agents...]      Add hooks to agents (claude codex cursor gemini hermes). Default: all found
  agent-ping uninstall [agents...]    Remove the hooks again
  agent-ping test                     Send a test notification
  agent-ping status                   Show settings and which agents are on / off
  agent-ping on [agents...]           Turn pings on (hooks the agent if needed). No agents: all
  agent-ping off [agents...]          Turn pings off, instantly, no restart needed. No agents: all
  agent-ping run -- <command>         Run any command, notify when it ends
  agent-ping send <title> [body]      Send a custom notification

  agent-ping use ntfy <topic> [server]
  agent-ping use bark <key-or-url>
  agent-ping use pushover <app-token> <user-key>
  agent-ping use telegram <bot-token> <chat-id>
  agent-ping use webhook <url>

  agent-ping set min_seconds <n>      Only notify if the task ran at least n seconds
  agent-ping set idle <n>             Only notify if you've been away from the keyboard n seconds
  agent-ping set include_text <true|false>   Add the start of the agent's reply
  agent-ping set approval <true|false>       Notify when an agent waits for your approval
  agent-ping set lang <en|zh>
  agent-ping set machine <name>       Name this computer in every notification ("-" to clear)
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "hook":
		runHook(args) // never fails loudly: hooks must not break the agent
		return
	case "setup":
		err = cmdSetup()
	case "_postinstall": // used by install.sh / install.ps1: first time → setup, upgrade → refresh hooks
		if cfg, _ := loadConfig(); len(cfg.Channels) > 0 {
			err = cmdInstall(nil)
		} else {
			err = cmdSetup()
		}
	case "use":
		err = cmdUse(args)
	case "set":
		err = cmdSet(args)
	case "install":
		err = cmdInstall(args)
	case "uninstall":
		err = cmdUninstall(args)
	case "on", "off":
		err = cmdSwitch(cmd == "on", args)
	case "test":
		err = cmdTest()
	case "send":
		err = cmdSend(args)
	case "run":
		os.Exit(cmdRun(args))
	case "status":
		err = cmdStatus()
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-ping:", err)
		os.Exit(1)
	}
}
