# Third-party and brand notices

The source code in this repository is MIT licensed. Product names and remote icon artwork remain the property of their respective owners; no affiliation, endorsement, or license for those brands is implied. Remote artwork is not vendored.

| Name | Official remote source | Format and verified public bytes | Usage |
|---|---|---|---|
| Codex | https://apps.apple.com/us/app/chatgpt/id6448311069 | JPEG, 512×512, SHA-256 `2721f36589650b13899312893b38f97b888fe11c266205ce0e4ce4473462cb87` | Remote provider decoration only; configurable or omitted. |
| Claude Code | https://apps.apple.com/us/app/claude-by-anthropic/id6473753684 | JPEG, 512×512, SHA-256 `f6c37ac6a5b952d9157e066c9a063cbd6f550e9a6db9b7a84b3044fbef21e7af` | Remote provider decoration only; configurable or omitted. |
| Cursor | https://apps.apple.com/us/app/cursor/id6767085653 | JPEG, 512×512, SHA-256 `ed41770f95fd61fd3138bed5feb46ad795491ecba3a4a3f7dae13c8635f335d0` | Remote provider decoration only; configurable or omitted. |
| Gemini CLI | https://apps.apple.com/us/app/google-gemini/id6477489729 | JPEG, 512×512, SHA-256 `98b0dc85fa41a37fcd58945d3374b2a290fd1e9839fe244b7e70c75e6f57180a` | Remote provider decoration only; configurable or omitted. |
| OpenCode | https://github.com/anomalyco/opencode/blob/b4147c8d08b2e14554337536f54c6965006b29ca/packages/desktop/icons/prod/ios/AppIcon-512@2x.png | PNG, 1024×1024, SHA-256 `cc7f380d7e0ef9fe76d99babdf91230b308b3740ff7f771c657248572c164860` | Pinned official commit; remote decoration only. |
| WorkBuddy | https://apps.apple.com/cn/app/id6761374913 | JPEG, 512×512, SHA-256 `776ab0ad30813d496b03e7ead4cebdb932c75549f07b5826b8b361b3c9ed783a` | Remote provider decoration only; configurable or omitted. |

All icons can be overridden with a user-controlled HTTPS URL, disabled with an empty override, or omitted when artwork cannot load. The fallback is text/default application artwork, never another Agent logo.

## Native security foundation dependencies

The native secrets package pins `golang.org/x/sys v0.47.0` (Windows DPAPI)
and `github.com/keybase/go-keychain v0.0.1` (macOS Keychain). The Windows
package links x/sys/windows; the macOS package links go-keychain and Apple's
system Security/CoreFoundation frameworks. The current minimal native CLI
does not yet import the secrets package; this foundation is not a complete
notification runtime. macOS execution remains an experimental CI gate.

The go-keychain module also declares upstream test dependencies
`github.com/stretchr/testify v1.10.0`, `github.com/davecgh/go-spew v1.1.1`,
`github.com/pmezard/go-difflib v1.0.0`, and
`gopkg.in/yaml.v3 v3.0.1`. Its Linux-only secretservice
dependencies are `github.com/keybase/dbus v0.0.0-20220506165403-5aa21ea2c23a`
and `golang.org/x/crypto v0.32.0`. These are not
linked into this project's Windows/macOS secrets package; Linux support and
running upstream system-Keychain tests are outside this foundation's scope.

### golang.org/x/sys — BSD-3-Clause

Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

### github.com/keybase/go-keychain — MIT

The MIT License (MIT)

Copyright (c) 2015 Keybase

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
