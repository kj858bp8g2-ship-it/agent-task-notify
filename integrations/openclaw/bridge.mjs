import { randomUUID } from "node:crypto"
import path from "node:path"
import { spawn } from "node:child_process"

const MAX_TRACKED = 2048
const CHILD_RETENTION_MS = 60 * 60 * 1000

function nonEmptyString(value) {
  return typeof value === "string" && value.trim() !== "" ? value : undefined
}

function absoluteString(value) {
  const normalized = nonEmptyString(value)
  return normalized && path.isAbsolute(normalized) ? normalized : undefined
}

function binaryName(platform) {
  return platform === "win32" ? "agent-task-notify.exe" : "agent-task-notify"
}

function boundedInsert(map, key, value) {
  map.delete(key)
  map.set(key, value)
  while (map.size > MAX_TRACKED) {
    map.delete(map.keys().next().value)
  }
}

export function createOpenClawBridge(options = {}) {
  const platform = options.platform ?? process.platform
  const pluginDirectory = absoluteString(options.pluginDirectory)
  const pluginConfig = options.pluginConfig && typeof options.pluginConfig === "object" ? options.pluginConfig : {}
  const configuredExecutable = Object.hasOwn(pluginConfig, "executable")
  const configuredDataDirectory = Object.hasOwn(pluginConfig, "dataDirectory")
  const executable = configuredExecutable
    ? absoluteString(pluginConfig.executable)
    : pluginDirectory && path.join(pluginDirectory, "runtime", binaryName(platform))
  const dataDirectory = configuredDataDirectory ? absoluteString(pluginConfig.dataDirectory) : undefined
  const configurationValid = Boolean(executable) && (!configuredDataDirectory || Boolean(dataDirectory))
  const spawnImpl = options.spawnImpl ?? spawn
  const randomUUIDImpl = options.randomUUIDImpl ?? randomUUID
  const now = options.now ?? Date.now

  const activeBySourceRun = new Map()
  const activeBySession = new Map()
  const childSessions = new Map()

  function launch(payload) {
    if (!configurationValid) return
    try {
      const args = ["hook", "--agent", "openclaw"]
      if (dataDirectory) args.push("--data-directory", dataDirectory)
      const child = spawnImpl(executable, args, {
        shell: false,
        stdio: ["pipe", "ignore", "ignore"],
        windowsHide: true,
      })
      child.on?.("error", () => {})
      child.stdin?.on?.("error", () => {})
      child.stdin?.end(`${JSON.stringify(payload)}\n`)
    } catch {
      // Notification failures must never block OpenClaw's fail-closed start hook.
    }
  }

  function pruneChildren() {
    const current = now()
    for (const [session, expiresAt] of childSessions) {
      if (expiresAt !== Number.POSITIVE_INFINITY && expiresAt <= current) childSessions.delete(session)
    }
  }

  function markChild(session, ended = false) {
    const value = nonEmptyString(session)
    if (!value) return
    pruneChildren()
    boundedInsert(childSessions, value, ended ? now() + CHILD_RETENTION_MS : Number.POSITIVE_INFINITY)
  }

  function isChild(session) {
    pruneChildren()
    return childSessions.has(session)
  }

  function contextSession(ctx) {
    return nonEmptyString(ctx?.sessionKey) ?? nonEmptyString(ctx?.sessionId)
  }

  function contextSourceRun(event, ctx) {
    return nonEmptyString(ctx?.runId) ?? nonEmptyString(event?.runId)
  }

  function addRecord(record) {
    if (record.sourceRun) boundedInsert(activeBySourceRun, record.sourceRun, record)
    const queue = activeBySession.get(record.sessionId) ?? []
    queue.push(record)
    while (queue.length > MAX_TRACKED) queue.shift()
    boundedInsert(activeBySession, record.sessionId, queue)
  }

  function removeRecord(record) {
    if (record.sourceRun && activeBySourceRun.get(record.sourceRun) === record) activeBySourceRun.delete(record.sourceRun)
    const queue = activeBySession.get(record.sessionId) ?? []
    const next = queue.filter((candidate) => candidate !== record)
    if (next.length === 0) activeBySession.delete(record.sessionId)
    else activeBySession.set(record.sessionId, next)
  }

  function beforeAgentRun(_event, ctx) {
    const sessionId = contextSession(ctx)
    if (!sessionId || isChild(sessionId)) return
    const sourceRun = contextSourceRun(undefined, ctx)
    if (sourceRun && activeBySourceRun.has(sourceRun)) return
    if (!sourceRun && (activeBySession.get(sessionId)?.length ?? 0) > 0) return
    const record = { sessionId, sourceRun, runId: sourceRun ?? randomUUIDImpl() }
    addRecord(record)
    launch({ schemaVersion: 1, event: "started", sessionId, runId: record.runId })
  }

  function agentEnd(event, ctx) {
    const sessionId = contextSession(ctx)
    if (!sessionId || isChild(sessionId) || typeof event?.success !== "boolean") return
    const sourceRun = contextSourceRun(event, ctx)
    let record = sourceRun ? activeBySourceRun.get(sourceRun) : undefined
    if (!record) record = activeBySession.get(sessionId)?.[0]
    if (!record || record.sessionId !== sessionId) return
    removeRecord(record)
    launch({
      schemaVersion: 1,
      event: event.success ? "stopped" : "failed",
      sessionId,
      runId: record.runId,
    })
  }

  return {
    beforeAgentRun,
    agentEnd,
    subagentSpawned(event) {
      markChild(event?.childSessionKey)
    },
    subagentProgress(event) {
      markChild(event?.childSessionKey, event?.phase === "ended")
    },
    subagentEnded(event) {
      markChild(event?.targetSessionKey, true)
    },
  }
}
