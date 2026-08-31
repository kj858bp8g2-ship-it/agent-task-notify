# Hermes Agent integration (experimental)

This self-contained integration uses Hermes Agent's official Shell Hooks: `pre_llm_call` starts timing once per turn, and `on_session_end` closes the turn on success, failure, or interruption. `turn_id` is used when Hermes supplies it. A child turn carrying `parent_session_id` is ignored.

Hermes sends a larger JSON envelope to every Shell Hook. `agent-task-notify` retains only the event name, session/turn IDs, and terminal booleans; message/history fields are discarded before runtime state is created. The package is contract-tested against the official Hermes hook wire format, but has not been run in a real Hermes installation by this project.

## Install

1. Configure `agent-task-notify` locally first. Never paste Bark/ntfy credentials into agent chat or Hermes YAML.
2. Copy `config.example.yaml` as a reference and merge its two entries into your existing `~/.hermes/config.yaml`. Do not overwrite other hooks.
3. Replace `__ATN_EXECUTABLE__` with the absolute path to `hermes/runtime/agent-task-notify.exe` on Windows or `hermes/runtime/agent-task-notify` on macOS. Replace `__ATN_DATA_DIRECTORY__` with the absolute local data directory chosen during notifier configuration.
4. Because Hermes runs commands through `shlex.split(..., shell=False)`, keep the surrounding double quotes. On native Windows, spell absolute paths with forward slashes (`C:/path/to/...`) so backslashes are not consumed as escapes.
5. Review and validate before starting a real task:

   ```text
   hermes hooks list
   hermes hooks doctor
   hermes hooks test pre_llm_call
   hermes hooks test on_session_end
   ```

   Hermes asks for consent the first time it sees each `(event, command)` pair. Approve only the two reviewed commands. This project does not recommend enabling global `hooks_auto_accept`.

6. Run `agent-task-notify preview --agent hermes --send` from the package root to verify phone delivery independently of Hermes.

## Remove

Remove only the two exact entries you added from `~/.hermes/config.yaml`, then run `hermes hooks doctor`. If desired, revoke their stored approvals with `hermes hooks revoke`. `agent-task-notify uninstall --agent hermes` intentionally refuses because the native CLI does not own Hermes YAML or its allowlist.

## Known boundaries

- No real-host Hermes E2E evidence is claimed.
- Reduced CLI exit events may omit `turn_id`; the runtime then closes the active session turn conservatively.
- Notifications remain threshold-, network-, provider-, OS-, and phone-policy-dependent.
