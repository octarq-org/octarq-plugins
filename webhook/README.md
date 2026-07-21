# octarq-plugin-webhook

A community **connector plugin** for [Octarq](https://github.com/octarq-org/octarq)
that bridges your instance to a Webhook URL:

- **📬 Inbound email → Webhook** — forward inbound email to your endpoint the moment
  mail lands on your domains (via the `OnEmail` hook + Octarq's built-in Webhook sender).
- **🤖 Agent-native** — exposes a `send_webhook` **MCP tool**, so an AI agent
  (Claude Code, Cursor, …) driving your Octarq can notify external endpoints directly.
- **⚙️ Settings page** — store a webhook URL per workspace, with a one-click test.

It's built entirely on the public `plugin.Context` seams — no fork, no
`internal/*` — so it doubles as a **reference connector**. See the backend in
[`plugin.go`](plugin.go) / [`mcp.go`](mcp.go) and the UI in [`web/`](web/).

## Setup

1. Have a webhook URL ready (e.g. `https://example.com/webhook`).
2. Compose this plugin into your Octarq build (see below), open **Webhook** in
   the dashboard, paste the URL, and hit **Send test**.

## Build it into Octarq

Plugins are composed at build time. With the core's build service:

```bash
OCTARQ_PLUGINS='[{"go":"github.com/octarq-org/octarq-plugins/webhook","npm":"@octarq/plugin-webhook"}]' make plugin-build
```

Or wire it by hand in a custom `main.go`:

```go
app.Use(&webhook.Plugin{})
```

and add `octarq-plugin-webhook` to the frontend plugin manifest.

## Develop

```bash
go build ./...   # backend, against the Octarq core
go vet ./...
```

The frontend type-check (`cd web && npx tsc --noEmit`) needs
`@octarq/plugin-sdk` from npm.

## License

[MIT](LICENSE).
