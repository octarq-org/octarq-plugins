# Octarq Plugins

Official plugins and the starter template for [Octarq](https://github.com/octarq-org/octarq)
— the self-hosted infrastructure for one-person companies & AI-native teams.

Each plugin is a full-stack feature composed into an Octarq binary **at build
time, without forking**: a Go module (`plugin.Plugin`) + a React package
(`UIPlugin` from [`@octarq/plugin-sdk`](https://www.npmjs.com/package/@octarq/plugin-sdk)).
Every plugin can also expose its tools to AI agents over MCP.

> Octarq core is infrastructure only. The links, mail and DNS features live in
> this repository. The Pro edition is built the same way. Anything they do, a
> community plugin can do too.

## Plugins

| Plugin | What it does |
|---|---|
| [**links**](links/) | **Short links** — custom and random slugs, geo/device routing, expiry and click limits, click analytics, UTM builder, QR codes, tags. |
| [**mail**](mail/) | **Mail** — mailboxes on your own domains, SMTP senders, inbound webhooks, templates, and a reading client. |
| [**dns**](dns/) | **DNS** — domains, providers, records, email authentication presets, and DDNS tokens. |
| [**twofa**](twofa/) | **2FA Vault** — centralized TOTP authenticator vault for infrastructure & shared accounts, AES-256-GCM encrypted, live countdown UI, and MCP tools for AI agents. |
| [**maillink**](maillink/) | **Agent-native demo** — auto-shortens the first link in each inbound email (via the links service) and exposes `list_email_links` over MCP. |
| [**_template**](_template/) | Starter to copy when writing your own plugin. |

## Use a plugin

Plugins are composed at build time (like `xcaddy`). From an Octarq core checkout:

```bash
OCTARQ_PLUGINS='[
  {"go":"github.com/octarq-org/octarq-plugins/dns","npm":"@octarq/plugin-dns"},
  {"go":"github.com/octarq-org/octarq-plugins/links","npm":"@octarq/plugin-links"},
  {"go":"github.com/octarq-org/octarq-plugins/mail","npm":"@octarq/plugin-mail"}
]' make plugin-build
```

`links` requires `dns`. `mail` requires `dns` and `links`. Mount them in that order.

## Develop

The Go modules resolve `github.com/octarq-org/octarq/server` from the version
pinned in each `go.mod`. To build against a local Octarq checkout, add it to
`go.work` (the file is gitignored), then run:

```bash
cd dns && go build ./... && go test ./... -race
pnpm install && pnpm check-types && pnpm test    # from the repository root
```

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
