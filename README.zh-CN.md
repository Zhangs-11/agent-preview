# agent-preview

[English](README.md) | [简体中文](README.zh-CN.md)

> AI 在远程机器上做好网页，你却还得下载 HTML 才能看。
> `agent-preview` 让 Codex 和 Claude Code 在交付成品时，自动附上一条本机浏览器可直接打开的临时链接。

<p align="center">
  <a href="https://github.com/Zhangs-11/agent-preview/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/Zhangs-11/agent-preview/ci.yml?branch=main&style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/network/members"><img alt="Forks" src="https://img.shields.io/github/forks/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/issues"><img alt="Issues" src="https://img.shields.io/github/issues/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="https://github.com/Zhangs-11/agent-preview/commits/main"><img alt="Last commit" src="https://img.shields.io/github/last-commit/Zhangs-11/agent-preview?style=flat-square" /></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/Zhangs-11/agent-preview?style=flat-square" /></a>
</p>

```text
你：帮我做一个产品介绍页面。

Agent：页面已经完成。
预览：https://example.lhr.life/?token=...
Provider：localhost.run
有效期：24 小时
文件：/workspace/product/dist
```

用户不用再说“帮我预览”，不用自己拼 `--public`，也不用把文件下载回本地。

## 三步安装

1. 安装 CLI：

   ```bash
   curl -fsSL https://raw.githubusercontent.com/Zhangs-11/agent-preview/main/install.sh | sh
   agent-preview version
   ```

2. 安装 Agent Skill：

   ```bash
   npx skills add Zhangs-11/agent-preview --skill agent-preview
   npx skills add Zhangs-11/agent-preview --list
   ```

3. Claude Code 用户可额外安装 Plugin：

   ```bash
   claude plugin marketplace add Zhangs-11/agent-preview
   claude plugin install agent-preview@agent-preview
   ```

安装后，正常和 Agent 对话即可。Skill 会在浏览器可查看的成品完成并验证后调用 CLI。

## 你可以这样说

- “帮我做一个产品落地页。”
- “在 `dist/` 做个带 CSS 和 JavaScript 的数据看板。”
- “生成一份 PDF 报告给我。”
- “把这个 React 页面改好，完成后给我看看。”

如果任务只有后端代码、普通源码或不可离开机器的敏感内容，Skill 不会创建公网预览。

## 它如何选择可用地址

```text
成品完成并验证
  → 本地服务仅监听 127.0.0.1
  → 已安装 cloudflared：尝试 Cloudflare Quick Tunnel
  → 公开健康检查确认用户侧域名真的可访问
  → Cloudflare 不可达：关闭其进程，自动切换 localhost.run
  → 返回实际 Provider、完整链接、文件路径和有效期
```

`localhost.run` 使用系统 SSH 客户端，不需要注册账号或下载额外程序。工具使用自己的 `known_hosts`，不会修改用户的全局 SSH 配置。

## 核心能力

- 单文件预览只暴露目标文件，不暴露同目录内容。
- 静态站点支持 `index.html`、`index.htm` 和本地 CSS、JavaScript、图片。
- 支持 PDF、PNG、JPEG、GIF、WebP、SVG、TXT。
- 可代理 Agent 明确启动的 `127.0.0.1` HTTP 端口，但从不扫描端口。
- 每个预览使用 256 位能力 Token；没有完整链接的请求返回 `401`。
- 首次验证后把 Token 从地址栏移除，保存到独立的 `HttpOnly` Cookie。
- 阻止隐藏文件、常见密钥文件、目录列表、路径穿越和符号链接越界。
- 后台 worker 默认运行 24 小时，也支持自定义时长和手动停止。
- JSON 输出包含实际 `provider`，不会把请求值冒充成真实成功结果。

## 前置条件

- [ ] macOS 或 Linux；发布包覆盖 amd64 和 arm64。
- [ ] 已有 `curl`、`tar` 和 SHA-256 工具：macOS 自带 `shasum`，多数 Linux 自带 `sha256sum`。
- [ ] 公网预览需要系统 `ssh`，或完成可选的 Cloudflare 设置。
- [ ] 服务器允许向公网发起 HTTPS 或 SSH 连接。
- [ ] Codex、Claude Code 或其他兼容 Agent Skills 的编程 Agent。

## 可选的 Cloudflare 设置

默认备用通道无需 Cloudflare 账号。如果希望优先使用 Cloudflare，请先阅读 [Cloudflare License](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/license/)、[Terms](https://www.cloudflare.com/terms/) 和 [Privacy Policy](https://www.cloudflare.com/privacypolicy/)，然后运行：

```bash
agent-preview setup --accept-cloudflare-terms
```

它会从 Cloudflare 官方 GitHub Release 下载 `cloudflared` 到用户缓存，并校验 GitHub Release API 返回的 SHA-256。不会安装系统服务，也不会修改既有 Cloudflare 配置。

Cloudflare 官方说明 Quick Tunnel 可能与 `~/.cloudflared/config.yaml` 冲突。工具不会重命名或覆盖该文件；Cloudflare 失败时会记录真实诊断并切换备用通道。

## 手动使用 CLI

```bash
# 单个自包含 HTML
agent-preview publish --ttl 24h ./index.html

# 带本地资源的静态站点
agent-preview publish --ttl 72h ./dist

# Agent 明确启动的开发服务器
agent-preview publish --port 3000 --ttl 24h

# 给 Agent 使用的结构化输出
agent-preview publish --json ./dist

# 一直保留到主动停止或机器重启
agent-preview publish --ttl 0 ./dist

agent-preview list
agent-preview stop <preview-id>
```

开发和测试时可使用 `--local-only`，完全不连接公网 Provider。

## 安全边界

完整预览链接是一种 bearer capability：拿到链接的人就能访问。不要预览密钥、客户数据、生产后台、私有 Prompt 或用户要求仅保存在本机的内容。

目录服务会阻止常见敏感路径，但任何 denylist 都无法识别所有业务私密文件。优先发布专门的构建目录，例如 `dist/`，不要发布仓库根目录。

匿名 `localhost.run` 连接在同一台机器上可能复用域名。为避免两个项目静默串流，备用通道同时只允许一个活动预览；先停止它才能发布下一条。Cloudflare 可用时没有这项限制。

用户设置的 TTL 是工具管理的最长时间。免费 Provider 可能提前结束连接，服务器重启后也不会恢复原地址。

## Troubleshooting

| 问题 | 原因 | 解决方法 |
|---|---|---|
| `trycloudflare.com` 无法打开 | 当前网络阻断了 Cloudflare 随机域名 | 工具会验证地址并自动切换 `localhost.run`；查看 JSON 中的 `provider` |
| `another localhost.run preview is active` | 匿名备用通道在同机复用域名 | `agent-preview list` 找到活动 ID，再运行 `agent-preview stop <id>` |
| 浏览器返回 `401` | 打开了不含 Token 的裸地址 | 使用 Agent 返回的完整链接；首次验证后 Token 会自动从地址栏移除 |
| Agent 没有自动返回链接 | Skill 未安装、CLI 不在 `PATH`，或成品类型不受支持 | 运行 `npx skills add Zhangs-11/agent-preview --list` 和 `agent-preview version` |
| `cloudflared` 启动失败 | 现有配置冲突、版本或网络问题 | 查看 CLI 返回的最近诊断；可直接让工具使用 `localhost.run` 备用通道 |

## 从源码构建

需要 Go 1.23 或更新版本：

```bash
git clone https://github.com/Zhangs-11/agent-preview.git
cd agent-preview
go test -race ./...
go build -o agent-preview ./cmd/agent-preview
```

发布前检查：

```bash
go test -race ./...
go vet ./...
claude plugin validate --strict .
```

当前 MVP 已在 macOS 上完成真实浏览器公网验收，并完成 Linux/macOS amd64、arm64 交叉构建。真实远程 Linux 服务器尚未做端到端验收。

## 致谢

- [Cloudflare cloudflared](https://github.com/cloudflare/cloudflared)：可选的 Quick Tunnel Provider。
- [localhost.run](https://localhost.run/docs/)：无需账号的 SSH 备用通道。
- [Agent Skills](https://github.com/vercel-labs/skills)：跨 Agent Skill 安装工具。

## License

MIT。`cloudflared` 与公共隧道服务受各自许可证、条款和隐私政策约束。
