#!/usr/bin/env bash
set -u
neutral() { printf '{"continue":true}\n'; }
root="${CODEBUDDY_PLUGIN_ROOT:-${CLAUDE_PLUGIN_ROOT:-}}"
case "$root" in
  /*|[A-Za-z]:/*|[A-Za-z]:\\*) ;;
  *) neutral; exit 0 ;;
esac
host=$(uname -s 2>/dev/null) || { neutral; exit 0; }
case "$host" in
  MINGW*|MSYS*|CYGWIN*) binary="$root/runtime/agent-task-notify.exe" ;;
  Darwin) binary="$root/runtime/agent-task-notify" ;;
  *) neutral; exit 0 ;;
esac
# Each candidate has exactly one platform binary. The OS rejects a wrong
# architecture; this experimental wrapper never falls back to another runtime.
if [ ! -f "$binary" ] || [ ! -x "$binary" ] || [ -L "$binary" ]; then
  neutral
  exit 0
fi
"$binary" hook --agent workbuddy 2>/dev/null || neutral
exit 0
