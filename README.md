# Octarq Plugins

Official plugins and the starter template for [Octarq](https://github.com/octarq-org/octarq)
— the self-hosted back office for one-person companies & AI-native teams.

Each plugin is a full-stack feature composed into an Octarq binary **at build
time, without forking**: a Go module (`plugin.Plugin`) + a React package
(`UIPlugin` from [`@octarq/plugin-sdk`](https://www.npmjs.com/package/@octarq/plugin-sdk)).
Every plugin can also expose its tools to AI agents over MCP.

> Octarq's own core features (links, mail, DNS) are built this exact way, and so
> is the Pro edition. Anything they do, a community plugin can too.

## Plugins

| Plugin | What it does |
|---|---|
| [**telegram**](telegram/) | Forward inbound email to a Telegram chat + a `send_telegram` MCP tool. |
| [**webhook**](webhook/) | Forward inbound email to any webhook URL (SSRF-hardened) + a `send_webhook` MCP tool. |
| [**maillink**](maillink/) | **Agent-native demo** — auto-shortens the first link in each inbound email (via the core `links.create` service) and exposes `list_email_links` over MCP. |
| [**twofa**](twofa/) | **2FA Vault** — centralized TOTP authenticator vault for infrastructure & shared accounts, AES-256-GCM encrypted, live countdown UI, and MCP tools for AI agents. |
| [**_template**](_template/) | Starter to copy when writing your own plugin. |

## Use a plugin

Plugins are composed at build time (like `xcaddy`). From an Octarq core checkout:

```bash
OCTARQ_PLUGINS='[{"go":"github.com/octarq-org/octarq-plugins/telegram","npm":"@octarq/plugin-telegram"}]' make plugin-build
```

Pass multiple entries to compose several at once.

## Write your own

1. Copy [`_template/`](_template/) (or use it as a reference).
2. Rename the module, package, and `Name()`; implement your feature on the
   public `plugin.Context` seams.
3. Full guide: [octarq/docs/PLUGINS.md](https://github.com/octarq-org/octarq/blob/main/docs/PLUGINS.md).

## Contributing

Community plugins are welcome — open a PR adding a subdirectory here, or publish
your own repo and open a PR listing it above. Plugins must be MIT/open-source,
build green (`go build ./...`), and document how to compose them.

## License

[MIT](LICENSE).
