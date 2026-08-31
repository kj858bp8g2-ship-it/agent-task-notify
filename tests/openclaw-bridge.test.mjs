import assert from "node:assert/strict"
import path from "node:path"
import test from "node:test"
import { createOpenClawBridge } from "../integrations/openclaw/bridge.mjs"

function harness(options = {}) {
  const calls = []
  const pluginDirectory = path.resolve("reviewed-openclaw")
  const bridge = createOpenClawBridge({
    platform: process.platform,
    pluginDirectory,
    randomUUIDImpl: () => "generated-run",
    spawnImpl(command, args, spawnOptions) {
      const call = { command, args, spawnOptions, input: "" }
      calls.push(call)
      return {
        on() {},
        stdin: {
          on() {},
          end(input) {
            call.input = input
          },
        },
      }
    },
    ...options,
  })
  return { bridge, calls, pluginDirectory }
}

function payload(call) {
  return JSON.parse(call.input)
}

test("bridges a parent run with exact private fields and shell disabled", () => {
  const { bridge, calls, pluginDirectory } = harness()
  bridge.beforeAgentRun({ prompt: "secret", messages: ["private"] }, { sessionKey: "session", runId: "run" })
  bridge.agentEnd({ success: true, messages: ["private"], error: "secret" }, { sessionKey: "session", runId: "run" })
  assert.equal(calls.length, 2)
  assert.deepEqual(calls[0].args, ["hook", "--agent", "openclaw"])
  const executable = process.platform === "win32" ? "agent-task-notify.exe" : "agent-task-notify"
  assert.equal(calls[0].command, path.join(pluginDirectory, "runtime", executable))
  assert.deepEqual(calls[0].spawnOptions, { shell: false, stdio: ["pipe", "ignore", "ignore"], windowsHide: true })
  assert.deepEqual(payload(calls[0]), { schemaVersion: 1, event: "started", sessionId: "session", runId: "run" })
  assert.deepEqual(payload(calls[1]), { schemaVersion: 1, event: "stopped", sessionId: "session", runId: "run" })
  assert.equal(calls[0].input.includes("secret"), false)
  assert.equal(calls[1].input.includes("private"), false)
})

test("correlates missing host run ids and reports failure", () => {
  const { bridge, calls } = harness()
  bridge.beforeAgentRun({}, { sessionId: "session" })
  bridge.agentEnd({ success: false, durationMs: 3600000 }, { sessionId: "session", runId: "late-host-id" })
  assert.equal(calls.length, 2)
  assert.equal(payload(calls[0]).runId, "generated-run")
  assert.deepEqual(payload(calls[1]), { schemaVersion: 1, event: "failed", sessionId: "session", runId: "generated-run" })
})

test("ignores observed child sessions and unmatched terminal events", () => {
  const { bridge, calls } = harness()
  bridge.subagentSpawned({ childSessionKey: "child" })
  bridge.beforeAgentRun({}, { sessionKey: "child", runId: "child-run" })
  bridge.agentEnd({ success: true }, { sessionKey: "child", runId: "child-run" })
  bridge.subagentEnded({ targetSessionKey: "child" })
  bridge.agentEnd({ success: true }, { sessionKey: "parent", runId: "unknown" })
  assert.equal(calls.length, 0)
})

test("invalid explicit paths disable launches without throwing", () => {
  const { bridge, calls } = harness({ pluginConfig: { executable: "relative", dataDirectory: "relative" } })
  bridge.beforeAgentRun({}, { sessionKey: "session", runId: "run" })
  bridge.agentEnd({ success: true }, { sessionKey: "session", runId: "run" })
  assert.equal(calls.length, 0)
})

test("passes reviewed absolute data directory and swallows spawn errors", () => {
  const dataDirectory = path.resolve("reviewed-data")
  let spawnedArgs
  const bridge = createOpenClawBridge({
    pluginDirectory: path.resolve("reviewed-openclaw"),
    pluginConfig: { dataDirectory },
    platform: process.platform,
    spawnImpl(_command, args) {
      spawnedArgs = args
      throw new Error("synthetic")
    },
  })
  assert.doesNotThrow(() => bridge.beforeAgentRun({}, { sessionKey: "session", runId: "run" }))
  assert.deepEqual(spawnedArgs, ["hook", "--agent", "openclaw", "--data-directory", dataDirectory])
  assert.doesNotThrow(() => bridge.agentEnd({ success: true }, { sessionKey: "session", runId: "run" }))
})
