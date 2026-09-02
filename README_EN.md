# agent-preview

> Codex or Claude Code finishes an HTML page on a remote machine, but you still have to download it before you can see it.
> `agent-preview` makes a browser-ready temporary URL part of the agent's final deliverable.

<p align="center">
  <a href="https://github.com/Zhangs-11/agent-preview/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/Zhangs-11/agent-preview/ci.yml?branch=main&style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/network/members"><img alt="Forks" src="https://img.shields.io/github/forks/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/issues"><img alt="Issues" src="https://img.shields.io/github/issues/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/commits/main"><img alt="Last commit" src="https://img.shields.io/github/last-commit/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/Zhangs-11/agent-preview?style=flat-square" /></a>
</p>

[中文](README.md) | **English**

## Install

Install the CLI:

```bash
curl -fsSL https://raw.githubusercontent.com/Zhangs-11/agent-preview/main/install.sh | sh
agent-preview version
```

Install the Agent Skill:

```bash
npx skills add Zhangs-11/agent-preview --skill agent-preview
```

Claude Code users can also install the plugin:

```bash
claude plugin marketplace add Zhangs-11/agent-preview
claude plugin install agent-preview@agent-preview
```

## Use it naturally

- “Build a product landing page for me.”
- “Create a static dashboard under `dist/`.”
- “Fix this React view and show me the finished result.”
- “Generate a PDF report.”

After verification, the agent publishes the narrowest safe target and returns the raw URL, actual provider, expiry, and artifact path.

## Provider behavior

When `cloudflared` is installed, the CLI tries Cloudflare and verifies its public health endpoint. If the generated address is unreachable, that process is stopped and the CLI falls back to `localhost.run` through the system SSH client. The preview origin always listens on `127.0.0.1`.

## Security and limitations

- The full URL is a bearer capability. Anyone who receives it can access the preview.
- Prefer a dedicated output directory instead of a repository root.
- Anonymous `localhost.run` previews are limited to one active tunnel per machine to prevent hostname collisions.
- The requested TTL is a maximum; a free provider may terminate earlier.
- Permanent URLs, office-document rendering, arbitrary TCP services, and automatic framework detection are outside the MVP.
- The core and real-browser public flow are verified on macOS. A real remote Linux server is not yet an end-to-end acceptance environment.

## Troubleshooting

| Problem | Fix |
|---|---|
| Cloudflare URL is unreachable | Let the automatic health check select `localhost.run`; inspect `provider` in JSON |
| Another localhost.run preview is active | Run `agent-preview list`, then `agent-preview stop <id>` |
| Browser returns `401` | Open the complete capability URL returned by the agent |
| The agent does not publish | Verify `agent-preview version` and `npx skills add Zhangs-11/agent-preview --list` |

## Development

```bash
go test -race ./...
go vet ./...
go build ./cmd/agent-preview
```

MIT licensed. Cloudflare and localhost.run remain governed by their own licenses and terms.
