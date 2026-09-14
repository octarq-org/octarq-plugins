# octarq-plugin-twofa

An open-source, enterprise-grade **2FA / TOTP Authenticator Vault plugin** for [Octarq](https://github.com/octarq-org/octarq) — the self-hosted back office for one-person companies & AI-native teams.

It centralizes two-factor authentication (TOTP) credentials for your infrastructure and shared accounts (AWS, Cloudflare, GitHub, registrars, cloud services), keeping seeds encrypted at rest with AES-256-GCM and exposing live codes to operators and AI agents (Cursor, Claude Code) over MCP.

---

## Features

- **🔐 Encrypted Storage at Rest**: Secrets are symmetrically encrypted using authenticated AES-256-GCM with unique cryptographic nonces before hitting the database.
- **⏱️ Real-Time TOTP Code Generation**: Generates standard RFC 6238 codes (6 or 8 digits) with animated 30s countdown rings and one-click copy.
- **🤖 Agent-Native MCP Tools**: Exposes `get_2fa_code`, `list_2fa_accounts`, and `verify_2fa_code` to connected AI agents so automated workflows can retrieve 2FA codes without friction.
- **📱 QR Code & URI Support**: Instant import from `otpauth://totp/...` URIs and on-demand QR code generation for pairing physical devices.
- **🏷️ Tags, Search & Pinning**: Filter credentials by tag, quick search, and pin high-frequency credentials to the top.
- **📋 Immutable Audit Trail**: Tracks every code generation, secret reveal, export, and MCP agent query.
- **🧩 Cross-Plugin Machine Contract**: Exposes `twofa.Provider` SPI (`twofa.provider`) for programmatic retrieval and verification across plugins.
- **🌐 Full Bilingual UI**: First-class English and Chinese interface powered by `@octarq/plugin-sdk`.

---

## Architecture & Documentation

Following the Octarq Three-Tier Documentation Architecture:
- **Tier 1: User / Admin Help Docs**: Bilingual guides in [`docs/twofa.md`](docs/twofa.md) and [`docs/twofa.zh.md`](docs/twofa.zh.md) embedded directly into the Go binary (`plugin.HelpDocsFS`).
- **Tier 2: Engineering Specification**: Technical specification in [`SPEC.md`](SPEC.md) documenting crypto algorithms, threat model, GORM database schema, fail-closed guards, and state machines (strictly NOT embedded).
- **Tier 3: Machine Contract**: Executable Go interface `Provider` in [`contract.go`](contract.go).

---

## Build into Octarq

Plugins are composed at build time without forking:

```bash
OCTARQ_PLUGINS='[{"go":"github.com/octarq-org/octarq-plugins/twofa","npm":"@octarq/plugin-twofa"}]' make plugin-build
```

Or wire manually in your custom `main.go`:

```go
import "github.com/octarq-org/octarq-plugins/twofa"

app.Use(&twofa.Plugin{})
```

---

## Development & Testing

### Backend Go Tests
```bash
go test -v ./...
go test -race ./...
go vet ./...
```

### Frontend Type-Check
```bash
cd web
pnpm exec tsc --noEmit
```

---

## MCP Tools Reference

Connected AI agents (Claude Code, Cursor) can call:

| Tool | Description | Parameters |
|---|---|---|
| `get_2fa_code` | Retrieve current live 6-digit TOTP code for an account | `account_name` (string, required) |
| `list_2fa_accounts` | List registered account names and tags | `tag` (string, optional) |
| `verify_2fa_code` | Verify whether a given TOTP code is valid | `account_name`, `code` (strings, required) |

---

## License

[MIT](LICENSE).
