---
name: agent-preview
description: Publish browser-previewable deliverables created by Codex or Claude Code and return a clickable URL. Use whenever the agent finishes an HTML or HTM file, static website, PDF, image, SVG, text artifact, or local web app that the user would otherwise need to download or cannot open from their own computer. Trigger after the deliverable is complete even when the user did not explicitly ask to preview it. Do not trigger for backend-only changes, ordinary source files, or content that should not leave the machine.
compatibility: Requires the agent-preview CLI on the machine that owns the files. Public previews require outbound network access plus either the system SSH client or optional cloudflared setup.
---

# Agent Preview

Treat a browser link as part of delivering a browser-previewable artifact. The user should not need to know the CLI or ask a second time for a preview.

## Publish after verification

Finish and verify the artifact before publishing it. Do not expose intermediate files while they are still changing.

Choose the narrowest target that preserves the result:

- Publish a single self-contained HTML, PDF, image, SVG, or text file by its exact path.
- Publish the generated output directory when an HTML page depends on local CSS, JavaScript, images, or routes. Use a dedicated output directory containing `index.html`; do not publish a repository root.
- Publish a port only when you started that HTTP development server for this task and know its exact port. Never scan for a port or expose an unrelated existing service.

Run the CLI with JSON output:

```bash
agent-preview publish --json --ttl 24h <absolute-file-or-directory>
```

For an HTTP service:

```bash
agent-preview publish --json --ttl 24h --port <known-port>
```

If the user requested a duration, translate it to a Go duration such as `72h`. Otherwise use 24 hours. Use `--ttl 0` only when the user explicitly wants the preview to remain until stopped.

## Resend an earlier preview

A preview that was ready earlier may no longer open: free providers can drop the connection or rotate the hostname. The worker checks the public URL every minute and marks the preview `error` after three consecutive failures. Before giving the user a URL from an earlier publish, run `agent-preview list --json` and confirm that preview's `status` is still `ready`. If it is not, publish the artifact again and give the user the new URL.

## Protect private material

The returned URL is a capability link: anyone who receives the complete link can use it. Do not publish credentials, customer data, production admin pages, private prompts, or artifacts the user said must remain local. A filename scan is not proof that content is safe.

If `agent-preview` is missing, Cloudflare setup is incomplete, the environment denies outbound access, or publishing fails, report the exact failure and the local artifact path. Do not claim that a preview exists.

## Final response

Read `url`, `expires_at`, `target`, and `provider` from the CLI's JSON. Put the raw URL on its own line so graphical clients and terminals can both recognize it.

```text
The page is ready.

Preview: https://preview-provider.example/?token=...
Provider: localhost.run
Expires: 24 hours
File: /absolute/path/to/dist
```

Do not print internal worker paths, cookies, or the token separately from the capability URL.
