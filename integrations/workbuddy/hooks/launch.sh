#!/usr/bin/env bash
set -eu
root="${CODEBUDDY_PLUGIN_ROOT:-${CLAUDE_PLUGIN_ROOT:-}}"
if [ -z "$root" ]; then
  printf '{"continue":true}\n'
  exit 0
fi
exec pwsh -NoLogo -NoProfile -NonInteractive -File "$root/runtime/scripts/agent-task-notify.ps1" -Mode Hook -Agent workbuddy
