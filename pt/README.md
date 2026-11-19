# octarq-plugin-pt

A small **private-tracker (PT) account manager plugin** for [Octarq](https://github.com/octarq-org/octarq).

It replaces the account side of a full downloader stack. The plugin keeps an encrypted copy of each site account, signs in again on demand, runs the daily sign-in, and reads the basic account numbers. It does not download torrents, watch RSS feeds, or manage a media library.

---

## Features

- **Encrypted accounts**: account data, browser cookies, and API keys are encrypted with a per-user, per-tracker key. The API never returns stored material.
- **Personal space**: each tracker belongs to one account and lives in that account's personal org. A team admin cannot see another member's tracker.
- **One-click login**: the server posts the login form and stores the new session.
- **Daily sign-in**: a window per site, one attempt per day, a ledger of every attempt.
- **Account data**: level, upload, download, ratio, bonus points, warnings, seeding and leeching counts.
- **Never a false success**: a page without positive evidence is reported as *not confirmed*, not as success.
- **Agent tools**: MCP tools for listing trackers, reading status, and running a sign-in.

## Site support

| Site type | Login | Sign-in | Data |
| --- | --- | --- | --- |
| NexusPHP (most PT sites) | Form login | Yes | Yes |
| M-Team | API key | No | Yes |
| HDDolby | API key | No | Yes |
| Rousi | API key | Yes | Yes |

M-Team and HDDolby have no public sign-in endpoint. Their sign-in is a browser-only signature, so the plugin declares it unsupported instead of guessing.

## Requirements

- Octarq Pro with this plugin in the build (`communityPlugins()`).
- A credential vault (`Host.Crypto()`). The plugin refuses to start without one.

## Documentation

- **Tier 1 help docs**: [`docs/pt.md`](docs/pt.md) and [`docs/pt.zh.md`](docs/pt.zh.md), embedded in the binary.
- **Tier 2 engineering spec**: [`SPEC.md`](SPEC.md), not embedded.

## Development

```bash
go build ./... && go vet ./... && go test ./... -race -count=1
cd web && pnpm check-types
```

The module pins `github.com/octarq-org/octarq/server` in `go.mod`. A local `go.work` can point it at a checkout instead.

## License

[MIT](LICENSE).
