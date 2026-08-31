# OpenClaw integration (experimental)

This self-contained plugin uses OpenClaw's typed `before_agent_run` and `agent_end` hooks. It passes only `schemaVersion`, lifecycle state, session ID, and a local run ID to `agent-task-notify`; prompts, messages, tool input/output, and model responses are discarded. Subagent sessions observed through OpenClaw's subagent lifecycle hooks are ignored.

The package is contract-tested against the official OpenClaw hook API, but has not been run in a real OpenClaw installation by this project. Review this local plugin before loading it: OpenClaw plugins execute inside the Gateway process.

## Install

1. Configure `agent-task-notify` locally first. Do not put Bark/ntfy credentials in OpenClaw configuration or agent chat.
2. Use an absolute path to this extracted `openclaw` directory:

   ```text
   openclaw plugins install --link <ABSOLUTE_PATH_TO_OPENCLAW_DIRECTORY> --force
   openclaw plugins enable agent-task-notify
   ```

3. Merge this entry into your existing `openclaw.json`; do not replace unrelated plugin settings:

   ```json
   {
     "plugins": {
       "entries": {
         "agent-task-notify": {
           "enabled": true,
           "hooks": { "allowConversationAccess": true }
         }
       }
     }
   }
   ```

   OpenClaw requires this explicit permission for both lifecycle hooks because their host events can contain conversation data. This plugin deliberately ignores those fields.

4. Restart and inspect:

   ```text
   openclaw gateway restart
   openclaw plugins inspect agent-task-notify --runtime --json
   ```

5. Run `agent-task-notify preview --agent openclaw --send` from the package root before relying on long-task delivery.

The plugin uses `runtime/agent-task-notify[.exe]` by default. Advanced users may set absolute `executable` and `dataDirectory` values under `plugins.entries["agent-task-notify"].config`. A relative or invalid explicit path disables notification launches instead of falling back silently.

## Remove

Disable/remove the plugin with OpenClaw's plugin CLI, remove only the `agent-task-notify` entry you added, and restart the Gateway. `agent-task-notify uninstall --agent openclaw` intentionally refuses because the native CLI does not own OpenClaw's configuration.

## Known boundaries

- No real-host OpenClaw E2E evidence is claimed.
- A Gateway restart during a run loses this plugin's in-memory run correlation, so that run may not notify.
- Notifications remain threshold-, network-, provider-, OS-, and phone-policy-dependent.
