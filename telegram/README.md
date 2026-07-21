# octarq-plugin-telegram

A community **connector plugin** for [Octarq](https://github.com/octarq-org/octarq)
that bridges your instance to Telegram:

- **📬 Inbound email → Telegram** — get pinged in a chat the moment mail lands on
  your domains (via the `OnEmail` hook + Octarq's built-in Telegram sender).
- **🤖 Agent-native** — exposes a `send_telegram` **MCP tool**, so an AI agent
  (Claude Code, Cursor, …) driving your Octarq can notify you directly.
- **⚙️ Settings page** — store a bot token (encrypted at rest) and chat ID per
  workspace, with a one-click test.

It's built entirely on the public `plugin.Context` seams — no fork, no
`internal/*` — so it doubles as a **reference connector**. See the backend in
[`plugin.go`](plugin.go) / [`mcp.go`](mcp.go) and the UI in [`web/`](web/).

## Setup

1. Create a bot with [@BotFather](https://t.me/BotFather) and copy its token.
2. Get your chat ID (e.g. message [@userinfobot](https://t.me/userinfobot)).
3. Compose this plugin into your Octarq build (see below), open **Telegram** in
   the dashboard, paste the token + chat ID, and hit **Send test**.

## Build it into Octarq

Plugins are composed at build time. With the core's build service:

```bash
OCTARQ_PLUGINS='[{"go":"github.com/octarq-org/octarq-plugins/telegram","npm":"@octarq/plugin-telegram"}]' make plugin-build
```

Or wire it by hand in a custom `main.go`:

```go
app.Use(&telegram.Plugin{})
```

and add `octarq-plugin-telegram` to the frontend plugin manifest.

## Develop

```bash
go build ./...   # backend, against the Octarq core
go vet ./...
```

The frontend type-check (`cd web && npx tsc --noEmit`) needs
`@octarq/plugin-sdk` from npm.

## License

[MIT](LICENSE).
