import { fileURLToPath } from "node:url"
import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry"
import { createOpenClawBridge } from "./bridge.mjs"

export default definePluginEntry({
  id: "agent-task-notify",
  name: "Agent Task Notify",
  description: "Notify a phone after a long OpenClaw agent run ends.",
  register(api) {
    const bridge = createOpenClawBridge({
      pluginDirectory: fileURLToPath(new URL(".", import.meta.url)),
      pluginConfig: api.pluginConfig,
    })
    api.on("subagent_spawned", bridge.subagentSpawned)
    api.on("subagent_progress", bridge.subagentProgress)
    api.on("subagent_ended", bridge.subagentEnded)
    api.on("before_agent_run", bridge.beforeAgentRun)
    api.on("agent_end", bridge.agentEnd)
  },
})
