# Privacy

Credentials are entered only locally, stored with Windows DPAPI CurrentUser protection, and kept outside the package in runtime data. Notification payloads exclude task prompts, output, file paths, provider credentials, and native identifiers. State filenames hash native identifiers.

The selected push service necessarily receives the configured service credential (for authentication/routing) and generic notification content. DPAPI protects storage on this Windows account; it is not end-to-end push encryption. An unrestricted ntfy topic may be subscribed to or published to by other people. Random names are not access control, and supplying a bearer token alone does not prove that the topic has ACLs. Configure and verify server-side topic ACLs; local unauthenticated opt-in accepts the exposure risk.

Configuration, install, doctor, and ordinary preview do not send pushes. A real push requires explicit `Preview -SendRealPush` or a user-enabled hook. There is no replay daemon and no exactly-once delivery claim.

The release scanner allows only generic project files, excludes `.git` and `.superpowers`, and rejects logs, DPAPI files, credential artifacts, machine-specific user paths, recognized task IDs, and release privacy markers. Native UUIDs are detected in identifier fields; OpenCode prefixed IDs are detected in native field context, while official artwork UUID URLs remain legal. This is a heuristic, not proof of anonymization: new ID formats, unlabelled IDs and secrets may evade it. Manually review every release file. Sanitise reports: include versions, safe Doctor output, and reproduction steps; never include keys, tokens, endpoints, prompt text, or local paths.

Installer backups retain exact original host-config bytes under runtime `backups/`, encrypted with DPAPI CurrentUser because host configs can contain secrets. Receipts record their location and manual-recovery purpose. Backup failure prevents hook activation. Uninstall only removes owned entries; it never restores a stale whole-file backup. Keep backups private and outside the release package.
