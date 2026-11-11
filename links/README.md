# octarq-plugin-links

An open-source, enterprise-grade **Short Links & Click Analytics plugin** for [Octarq](https://github.com/octarq-org/octarq).

It provides branded short URLs, routing rules (device, country, language), password protection, click-caps, expiry schedules, high-performance click aggregation with batch buffering, QR codes, CSV exports, and agent-native MCP tools.

---

## Features

- **🔗 Branded Short URLs**: Generate short URLs on workspace apex or dedicated custom domains.
- **🚦 Smart Routing Rules**: Route traffic based on device types, geographic locations, languages, or weighted split testing.
- **🛡️ Passcode & Safety Guards**: Protect links with passcodes, click caps, expiry dates, and anti-abuse safeguards.
- **📊 Real-time Click Analytics**: Non-blocking in-memory click buffering flushing asynchronously to SQLite/Postgres.
- **🤖 Agent-Native MCP Tools**: Expose link management (`list_links`, `create_link`, `get_link_stats`, `archive_link`) to AI agents.
- **🧩 Cross-Plugin SPI**: Publishes `plugin.LinkCreator` under `"links.create"` and `plugin.LinkResolver` under `plugin.ServiceLinkResolve`.

---

## Architecture & Documentation

Following the Octarq Three-Tier Documentation Architecture:
- **Tier 1: User / Admin Help Docs**: Bilingual guides in [`docs/short-links.md`](docs/short-links.md) embedded directly into the Go binary (`plugin.HelpDocsFS`).
- **Tier 2: Engineering Specification**: Technical specification in [`SPEC.md`](SPEC.md) documenting high-concurrency engine, state machines, and compound indexes (unembedded).
- **Tier 3: Machine Contract**: Executable Go interface assertion in [`contract.go`](contract.go).

---

## License

[MIT](LICENSE).
