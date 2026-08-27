---
name: agent-task-notify
description: Install or safely configure private phone notifications for long local Agent tasks on Windows. Use when the user explicitly asks to set up, inspect, preview, or remove Agent Task Notify; never request device credentials in chat.
---

# Agent Task Notify

Work only on the user's local Windows machine. Read [README.md](../../README.md) first for supported adapters and privacy boundaries.

Route by the user's requested action; inspection, preview and removal must not install or configure anything first. Preserve the same explicit `-DataDirectory` / `ATN_DATA_DIRECTORY` across actions.

- **Install:** establish adapter and installation route first. For Codex choose plugin loading OR `scripts/Install-Notifications.ps1`, never both. The plugin route uses the host's plugin UI and manual hook review/trust; never bypass trust. For script routes run the installer only for the chosen adapter. WorkBuddy is a manually reviewed experimental package, not automatic settings mutation. Automatic plugin/script duplicate detection is unavailable.
- **Configure:** only run `scripts/Configure-Notifications.ps1` for the selected provider. Have the user enter endpoint/token locally, never in chat. Explain that the selected service receives its credential and generic content; DPAPI is local storage protection, not end-to-end push encryption. Unrestricted ntfy topics can be read/published by others; random names are not ACLs and a bearer token does not prove ACL configuration. Recommend verified topic ACLs before local opt-in.
- **Inspect:** run `scripts/agent-task-notify.ps1 -Mode Doctor` only. `receiptPresent` means installer ownership, not proof of host loading. Do not install as a prerequisite.
- **Preview:** use `scripts/agent-task-notify.ps1 -Mode Preview -Agent <adapter>` for a dry preview. Only add `-SendRealPush` when explicitly requested. Enabled lifecycle hooks also send automatically when thresholds are met; manual real preview is not the sole sending path.
- **Remove:** for script installation run `scripts/Uninstall-Notifications.ps1 -Agent <adapter>`; preserve user settings/credentials and report edited-owned-entry conflicts. For plugin installation use the host's disable/remove workflow, preserving trust gates. Do not install first, restore a whole stale config backup, or delete runtime data by default.

Native adapters do not create `needs_attention` events. Sources lacking a native run ID can have an ambiguous delayed Stop, so do not claim exact task correlation. Refer to the shipped documentation for configuration, compatibility, privacy, and troubleshooting.
