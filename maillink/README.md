# octarq-plugin-maillink — the agent-native demo

The flagship **"look what a plugin can do"** demo for [Octarq](https://github.com/octarq-org/octarq).

**When an email arrives → Octarq auto-shortens the first link in it → your AI agent
can grab it over MCP.** No bespoke integration, ~a screenful of Go.

```
📬 inbound email  ──▶  maillink (OnEmail hook)
                        ├─ finds the first URL in the body
                        ├─ shortens it via the core links.create service
                        └─ records it
                                  │
🤖 Claude Code ──▶ MCP tool `list_email_links` ──▶ "here's the short link from your latest email"
```

It touches four public `plugin.Context` seams and nothing else:

- **`OnEmail`** — the inbound-mail hook.
- **`LookupAs[plugin.LinkCreator]`** — the core `links.create` service (no import of the links plugin).
- **a GORM `Model`** — auto-migrated, records each auto-created link.
- **`MCPProvider`** — exposes `list_email_links` so agents can read them.

That's the pitch of Octarq being a *framework*: this is the whole plugin.

## Try it

1. Compose it into your Octarq build (needs the `links` core plugin, which ships by default):
   ```bash
   OCTARQ_PLUGINS='[{"go":"github.com/octarq-org/octarq-plugins/maillink","npm":"@octarq/plugin-maillink"}]' make plugin-build
   ```
2. Send an email containing a URL to a mailbox on your domain.
3. Open **Mail Links** in the dashboard — or ask your MCP-connected agent to call
   `list_email_links`.

## Develop

```bash
go build ./...   # backend, against the Octarq core (>= v0.3.0 for links.create)
go vet ./...
```

Requires Octarq **v0.3.0+** (the release that added the `links.create` service).
Frontend type-check needs `@octarq/plugin-sdk` from npm.

## License

[MIT](LICENSE).
