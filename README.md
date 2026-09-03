# agent-preview

[English](README.md) | [简体中文](README.zh-CN.md)

> Codex or Claude Code finishes a web page on a remote machine, but you still have to download the HTML before you can see it.
> `agent-preview` makes a browser-ready temporary URL part of the agent's final deliverable.

<p align="center">
  <a href="https://github.com/Zhangs-11/agent-preview/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/Zhangs-11/agent-preview/ci.yml?branch=main&style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/network/members"><img alt="Forks" src="https://img.shields.io/github/forks/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/issues"><img alt="Issues" src="https://img.shields.io/github/issues/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/commits/main"><img alt="Last commit" src="https://img.shields.io/github/last-commit/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/Zhangs-11/agent-preview?style=flat-square" /></a>
</p>

```text
You: Build a product landing page for me.

Agent: The page is ready.
Preview: https://example.lhr.life/?token=...
Provider: localhost.run
Expires in: 24 hours
Files: /workspace/product/dist
```

Users no longer need to ask for a preview, assemble `--public` flags, or download finished files to their own machine.

## Install in three steps

1. Install the CLI:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/Zhangs-11/agent-preview/main/install.sh | sh
   agent-preview version
   ```

2. Install the Agent Skill:

   ```bash
   npx skills add Zhangs-11/agent-preview --skill agent-preview
   npx skills add Zhangs-11/agent-preview --list
   ```

3. Claude Code users can also install the plugin:

   ```bash
   claude plugin marketplace add Zhangs-11/agent-preview
   claude plugin install agent-preview@agent-preview
   ```

After installation, interact with the agent normally. Once a browser-viewable artifact is complete and verified, the skill invokes the CLI.

## Example prompts

- “Build a product landing page for me.”
- “Create a dashboard with CSS and JavaScript under `dist/`.”
- “Generate a PDF report.”
- “Fix this React view and show me the finished result.”

The skill does not create a public preview for backend-only code, ordinary source files, or sensitive content that must remain on the machine.

## How it selects a working URL

```text
Artifact completed and verified
  → Local server listens only on 127.0.0.1
  → cloudflared installed: try a Cloudflare Quick Tunnel
  → Public health check confirms the user-facing hostname is reachable
  → Cloudflare unreachable: stop its process and fall back to localhost.run
  → Return the actual provider, complete URL, file path, and expiry
```

`localhost.run` uses the system SSH client and requires no account or additional download. The tool uses its own `known_hosts` file and never modifies the user's global SSH configuration.

## Features

- A single-file preview exposes only the target file, not neighboring files.
- Static sites support `index.html`, `index.htm`, and local CSS, JavaScript, and images.
- Supports PDF, PNG, JPEG, GIF, WebP, SVG, and TXT.
- Can proxy an HTTP port explicitly started by the agent on `127.0.0.1`, but never scans ports.
- Every preview uses a 256-bit capability token; requests without the complete URL receive `401`.
- After the first verification, removes the token from the address bar and stores it in a dedicated `HttpOnly` cookie.
- Blocks hidden files, common secret files, directory listings, path traversal, and symlink escapes.
- Background workers run for 24 hours by default, with custom durations and manual stopping available.
- JSON output reports the actual `provider` instead of presenting the requested value as a successful result.

## Requirements

- [ ] macOS or Linux; release packages cover amd64 and arm64.
- [ ] `curl`, `tar`, and a SHA-256 utility: macOS includes `shasum`, and most Linux distributions include `sha256sum`.
- [ ] Public previews require the system `ssh` client or the optional Cloudflare setup.
- [ ] The server must allow outbound HTTPS or SSH connections.
- [ ] Codex, Claude Code, or another coding agent compatible with Agent Skills.

## Optional Cloudflare setup

The default fallback requires no Cloudflare account. To prefer Cloudflare, first read the [Cloudflare License](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/license/), [Terms](https://www.cloudflare.com/terms/), and [Privacy Policy](https://www.cloudflare.com/privacypolicy/), then run:

```bash
agent-preview setup --accept-cloudflare-terms
```

The command downloads `cloudflared` from Cloudflare's official GitHub Release into the user cache and verifies the SHA-256 returned by the GitHub Releases API. It does not install a system service or modify existing Cloudflare configuration.

Cloudflare documents that Quick Tunnels may conflict with `~/.cloudflared/config.yaml`. The tool never renames or overwrites that file. When Cloudflare fails, it records the real diagnostic and switches to the fallback provider.

## Manual CLI usage

```bash
# One self-contained HTML file
agent-preview publish --ttl 24h ./index.html

# A static site with local assets
agent-preview publish --ttl 72h ./dist

# A development server explicitly started by the agent
agent-preview publish --port 3000 --ttl 24h

# Structured output for agents
agent-preview publish --json ./dist

# Keep it until manually stopped or the machine restarts
agent-preview publish --ttl 0 ./dist

agent-preview list
agent-preview stop <preview-id>
```

Use `--local-only` during development and testing to avoid connecting to any public provider.

## Security boundaries

The complete preview URL is a bearer capability: anyone who receives it can access the preview. Never preview secrets, customer data, production admin panels, private prompts, or content the user requires to remain on the local machine.

The directory server blocks common sensitive paths, but no denylist can identify every business-confidential file. Prefer a dedicated build directory such as `dist/`; do not publish the repository root.

Anonymous `localhost.run` connections on one machine may reuse a hostname. To prevent two projects from silently serving through the same address, the fallback provider permits only one active preview at a time. Stop it before publishing another. This limitation does not apply when Cloudflare is available.

A user-specified TTL is the maximum duration managed by the tool. A free provider may terminate a connection earlier, and an address is not restored after the server restarts.

## Troubleshooting

| Problem | Cause | Fix |
|---|---|---|
| A `trycloudflare.com` URL does not open | The current network blocks random Cloudflare hostnames | The tool verifies the URL and automatically switches to `localhost.run`; inspect `provider` in the JSON output |
| `another localhost.run preview is active` | Anonymous fallback tunnels on one machine can reuse a hostname | Run `agent-preview list` to find the active ID, then `agent-preview stop <id>` |
| The browser returns `401` | The URL does not contain the capability token | Open the complete URL returned by the agent; the token is removed from the address bar after the first verification |
| The agent does not return a URL automatically | The skill is missing, the CLI is not in `PATH`, or the artifact type is unsupported | Run `npx skills add Zhangs-11/agent-preview --list` and `agent-preview version` |
| `cloudflared` fails to start | Existing configuration, version, or network problem | Inspect the latest CLI diagnostic; the tool can use `localhost.run` directly |

## Build from source

Go 1.23 or later is required:

```bash
git clone https://github.com/Zhangs-11/agent-preview.git
cd agent-preview
go test -race ./...
go build -o agent-preview ./cmd/agent-preview
```

Before release:

```bash
go test -race ./...
go vet ./...
claude plugin validate --strict .
```

The MVP has completed real-browser public acceptance testing on macOS and cross-builds for Linux/macOS amd64 and arm64. End-to-end testing on a real remote Linux server remains unverified.

## Acknowledgments

- [Cloudflare cloudflared](https://github.com/cloudflare/cloudflared): optional Quick Tunnel provider.
- [localhost.run](https://localhost.run/docs/): account-free SSH fallback.
- [Agent Skills](https://github.com/vercel-labs/skills): cross-agent skill installer.

## License

MIT. `cloudflared` and public tunnel services remain governed by their own licenses, terms, and privacy policies.
