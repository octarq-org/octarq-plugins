# octarq-plugin-mail

An open-source, enterprise-grade **Transactional Email & Mailbox plugin** for [Octarq](https://github.com/octarq-org/octarq).

It provides virtual mailboxes, RFC822 MIME parsing, inbound webhook adapters, outbound SMTP relaying, suppression list management, click wrapping via `plugin.LinkCreator`, and agent-native MCP tools.

---

## Features

- **📬 Virtual Mailboxes & Catch-All**: Multi-tenant virtual mailbox routing with catch-all support.
- **📥 Inbound Webhooks & Generic Adapters**: Process incoming emails from SendGrid, Postmark, Resend, or direct SMTP relays.
- **📤 Outbound SMTP Relay**: Authenticated outbound message delivery with anti-injection headers and TLS enforcement.
- **🚫 Suppression Lists**: Auto-suppress bounced, complained, or unsubscribed addresses.
- **🤖 Agent-Native MCP Tools**: Expose mailbox queries and drafting capabilities (`list_mailboxes`, `list_emails`, `send_email`) to AI agents.
- **🧩 Cross-Plugin Seams**: Emits `email.receive` events, wraps short links via `plugin.LinkCreator` SPI ("links.create"), and exports `NewDBStorageProvider(db)` for object storage backends.

---

## Architecture & Documentation

Following the Octarq Three-Tier Documentation Architecture:
- **Tier 1: User / Admin Help Docs**: Bilingual guides in [`docs/mailboxes.md`](docs/mailboxes.md) embedded directly into the Go binary (`plugin.HelpDocsFS`).
- **Tier 2: Engineering Specification**: Technical specification in [`SPEC.md`](SPEC.md) documenting database schemas, fail-closed guards, and RFC822 limits (unembedded).
- **Tier 3: Machine Contract**: Executable Go interface assertions in [`contract.go`](contract.go).

---

## License

[MIT](LICENSE).
