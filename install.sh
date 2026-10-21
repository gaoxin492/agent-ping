#!/bin/sh
# agent-ping installer for macOS / Linux:
#   curl -fsSL https://raw.githubusercontent.com/gaoxin492/agent-ping/main/install.sh | sh
set -e

REPO="${AGENT_PING_REPO:-gaoxin492/agent-ping}"
DIR="${AGENT_PING_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $os (on Windows use install.ps1)"; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported CPU: $(uname -m)"; exit 1 ;;
esac

url="https://github.com/$REPO/releases/latest/download/agent-ping_${os}_${arch}"
echo "Downloading $url"
mkdir -p "$DIR"
curl -fsSL "$url" -o "$DIR/agent-ping.tmp"
chmod +x "$DIR/agent-ping.tmp"
mv "$DIR/agent-ping.tmp" "$DIR/agent-ping"
echo "Installed to $DIR/agent-ping"

case ":$PATH:" in
  *":$DIR:"*) ;;
  *)
    case "$SHELL" in
      */zsh) rc="$HOME/.zshrc" ;;
      *)     rc="$HOME/.bashrc" ;;
    esac
    grep -q 'agent-ping PATH' "$rc" 2>/dev/null || \
      printf '\nexport PATH="%s:$PATH"  # agent-ping PATH\n' "$DIR" >> "$rc"
    echo "Added $DIR to PATH (open a new terminal to use the agent-ping command)."
    ;;
esac

# Interactive setup needs a keyboard even though this script came through a pipe.
if [ -r /dev/tty ] && [ -w /dev/tty ]; then
  "$DIR/agent-ping" _postinstall < /dev/tty
else
  echo "Next: run  agent-ping setup"
fi
