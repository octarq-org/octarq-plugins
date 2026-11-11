# octarq-plugin-dns

An open-source, enterprise-grade **DNS & Domain Management plugin** for [Octarq](https://github.com/octarq-org/octarq).

It manages domain zones, connects DNS providers (Cloudflare, DNSPod), provides dynamic DNS (DDNS) update tokens, generates email DNS blueprints, and exposes domain operations to operators and AI agents over MCP.

---

## Features

- **🌐 Domain Lifecycle Management**: Register, verify, and monitor custom domains across workspace tenants.
- **🔌 Multi-Provider Synchronization**: Synchronize zones and DNS records with Cloudflare and DNSPod.
- **🔄 Dynamic DNS (DDNS)**: Secure per-token DDNS endpoints for dynamic IP updates from edge routers and servers.
- **📧 Email Blueprints**: Automatically generate and verify MX, SPF, DKIM, and DMARC records.
- **🤖 Agent-Native MCP Tools**: Expose domain and record operations (`list_domains`, `get_domain`, `sync_provider`) to AI agents.
- **🧩 Cross-Plugin SPI**: Exposes `plugin.DNSManager` service contract.

---

## Architecture & Documentation

Following the Octarq Three-Tier Documentation Architecture:
- **Tier 1: User / Admin Help Docs**: Bilingual guides in [`docs/dns.md`](docs/dns.md) and [`docs/ddns.md`](docs/ddns.md) embedded directly into the Go binary (`plugin.HelpDocsFS`).
- **Tier 2: Engineering Specification**: Technical specification in [`SPEC.md`](SPEC.md) documenting database schemas, indexes, and fail-closed security guards (unembedded).
- **Tier 3: Machine Contract**: Executable Go contracts in [`contract.go`](contract.go).

---

## License

[MIT](LICENSE).
