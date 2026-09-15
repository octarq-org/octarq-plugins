# Octarq 2FA Vault Plugin — Engineering Specification

| Attribute | Value |
|---|---|
| **Plugin ID** | `twofa` |
| **Go Module** | `github.com/octarq-org/octarq-plugins/twofa` |
| **UI Package** | `@octarq/plugin-twofa` |
| **Tier Classification** | Tier 2 (Module Engineering Spec — strictly NOT embedded in Go binary) |
| **Version** | `1.0.0` |

---

## 1. Overview & Problem Statement

Modern developer teams and solo operators manage numerous critical services protected by Two-Factor Authentication (AWS, Cloudflare, GitHub, registrars, Stripe, payment rails). Storing TOTP seeds on individual personal mobile devices or browser extensions creates significant operational silos:
- Lack of centralized backup and team sharing for shared administrative accounts.
- AI agents (Cursor, Claude Code) operating in autonomous or semi-autonomous workflows cannot retrieve TOTP codes when performing deployment or verification tasks.
- No unified audit logging of who accessed or generated sensitive 2FA codes.

The **2FA Vault** plugin provides a centralized, multi-tenant TOTP authenticator vault composed into Octarq at build time. It provides authenticated AES-256-GCM encryption at rest, RFC 6238-compliant real-time code generation, a responsive glass-themed UI with countdown rings, a Model Context Protocol (MCP) tool suite, and an immutable audit log.

---

## 2. Cryptographic Architecture & Security Model

### 2.1 Encryption at Rest (AES-256-GCM)

All TOTP secret seeds are encrypted before persistence in the database:
- **Algorithm**: AES-256 in Galois/Counter Mode (GCM).
- **Key Derivation**: 32-byte key derived via SHA-256 with a domain-separated salt:
  `Key = SHA-256("octarq-twofa-vault-encryption-salt-v1" || MasterSecret)`
  `MasterSecret` is sourced from `OCTARQ_TWOFA_KEY`, `OCTARQ_SECRET_KEY`, or an instance-seeded fallback.
- **Nonce (IV)**: 12-byte cryptographically secure random bytes generated uniquely per encryption operation via `crypto/rand`.
- **Ciphertext Storage Format**: Base64 encoding of `[12-byte Nonce || Ciphertext + 16-byte GCM Authentication Tag]`.

### 2.2 Fail-Closed Principle & Decryption Integrity

- If the ciphertext is truncated, malformed, or tampered with, GCM authentication tag verification fails immediately.
- Decryption failures fail closed (`ErrDecryptionFailed`) and never return partially decrypted or corrupt data.
- Plaintext secrets are strictly excluded from:
  - Standard list endpoints (`GET /api/twofa/accounts`).
  - Account summaries returned to MCP agents.
  - Inbound and outbound application logs.
- Plaintext secrets can only be revealed via explicit invocation (`GET /api/twofa/accounts/{id}?reveal=true`) or QR code export, both of which trigger high-priority audit logs.

---

## 3. RFC 6238 TOTP Engine Specification

The plugin implements RFC 6238 (TOTP: Time-Based One-Time Password Algorithm) and RFC 4226 (HOTP):
- **Time Step ($T_X$)**: Default 30 seconds (configurable per account: 15s, 30s, 60s).
- **Counter ($T$)**:
  $$T = \lfloor (\text{UnixTime} - T_0) / T_X \rfloor \quad \text{where } T_0 = 0$$
- **HMAC Computation**: Supports `SHA1` (RFC default), `SHA256`, and `SHA512`.
- **Dynamic Truncation**:
  - Offset extracted from lowest 4 bits of the HMAC digest: $\text{offset} = \text{digest}[\text{len}-1] \ \& \ 0x0F$.
  - 31-bit integer extracted:
    $$\text{P} = (\text{digest}[\text{offset}] \ \& \ 0x7F) \ll 24 \ | \ (\text{digest}[\text{offset}+1] \ \& \ 0xFF) \ll 16 \ | \ (\text{digest}[\text{offset}+2] \ \& \ 0xFF) \ll 8 \ | \ (\text{digest}[\text{offset}+3] \ \& \ 0xFF)$$
  - Code modulo $10^{\text{digits}}$ (6 or 8 digits), formatted with leading zeros.
- **Clock Drift Tolerance**:
  - Verification allows a configurable drift window: checks steps $T-1$, $T$, and $T+1$ (window of $\pm 30\text{s}$) to tolerate reasonable client/server clock skew.
  - Constant-time string comparison (`crypto/subtle.ConstantTimeCompare`) prevents timing side-channel attacks.

---

## 4. Database Schema & Models

### 4.1 `two_fa_accounts` Table

```sql
CREATE TABLE two_fa_accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    updated_at DATETIME,
    deleted_at DATETIME,
    org_id INTEGER NOT NULL,
    name VARCHAR(255) NOT NULL,
    issuer VARCHAR(255),
    account VARCHAR(255),
    encrypted_secret TEXT NOT NULL,
    algorithm VARCHAR(32) DEFAULT 'SHA1',
    digits INTEGER DEFAULT 6,
    period INTEGER DEFAULT 30,
    tags VARCHAR(512),
    notes TEXT,
    pinned BOOLEAN DEFAULT 0
);

CREATE INDEX idx_two_fa_accounts_org_id ON two_fa_accounts(org_id);
CREATE INDEX idx_two_fa_accounts_pinned ON two_fa_accounts(pinned);
CREATE INDEX idx_two_fa_accounts_deleted_at ON two_fa_accounts(deleted_at);
```

### 4.2 `two_fa_audit_logs` Table

```sql
CREATE TABLE two_fa_audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME,
    org_id INTEGER NOT NULL,
    account_id INTEGER,
    account_name VARCHAR(255),
    action VARCHAR(64) NOT NULL,
    actor VARCHAR(255),
    ip VARCHAR(64)
);

CREATE INDEX idx_two_fa_audit_logs_org_id ON two_fa_audit_logs(org_id);
CREATE INDEX idx_two_fa_audit_logs_created_at ON two_fa_audit_logs(created_at);
CREATE INDEX idx_two_fa_audit_logs_action ON two_fa_audit_logs(action);
```

---

## 5. REST API Specifications

All endpoints are mounted under `/api/twofa/` and guarded with `ctx.Guard`:

| Method | Path | Description | Audit Action |
|---|---|---|---|
| `GET` | `/api/twofa/accounts` | List all accounts with live codes & remaining seconds. Secret excluded. | — |
| `POST` | `/api/twofa/accounts` | Create account (manual input or `otpauth_url`). Encrypts secret. | `create_account` |
| `GET` | `/api/twofa/accounts/{id}` | Get account details. With `?reveal=true`, returns decrypted secret. | `view_secret` (if reveal) |
| `PUT` | `/api/twofa/accounts/{id}` | Update account metadata or update secret seed. | `update_account` |
| `DELETE` | `/api/twofa/accounts/{id}` | Soft delete account. | `delete_account` |
| `POST` | `/api/twofa/accounts/{id}/pin` | Toggle pinned state. | — |
| `POST` | `/api/twofa/accounts/{id}/code` | Generate current live TOTP code on demand. | `generate_code` |
| `POST` | `/api/twofa/accounts/{id}/verify` | Verify submitted code against account with drift tolerance. | `verify_code` |
| `GET` | `/api/twofa/accounts/{id}/qr` | Return QR code (image PNG or JSON Data URL). | `view_qr` |
| `POST` | `/api/twofa/import` | Batch import array of `otpauth://totp/...` URIs. | `import_accounts` |
| `GET` | `/api/twofa/export` | Export accounts with URIs for migration/backup. | `export_accounts` |
| `GET` | `/api/twofa/logs` | Fetch last 50 audit log events for active workspace. | — |

---

## 6. Model Context Protocol (MCP) Tool Contract

Exposed via `RegisterMCP(srv *mcp.Server)`:

### `get_2fa_code`
- **Description**: Generate the current live 6-digit TOTP code for an account.
- **Input Schema**:
  ```json
  {
    "type": "object",
    "properties": {
      "account_name": { "type": "string", "description": "Name or issuer of the account (e.g. AWS, GitHub)" }
    },
    "required": ["account_name"]
  }
  ```
- **Output**: Formatted code string and seconds remaining until expiration.

### `list_2fa_accounts`
- **Description**: List registered account metadata (name, issuer, tags, pinned) without secrets or codes.
- **Input Schema**:
  ```json
  {
    "type": "object",
    "properties": {
      "tag": { "type": "string", "description": "Optional tag filter" }
    }
  }
  ```

### `verify_2fa_code`
- **Description**: Verify whether a given TOTP code is valid for an account.
- **Input Schema**:
  ```json
  {
    "type": "object",
    "properties": {
      "account_name": { "type": "string" },
      "code": { "type": "string" }
    },
    "required": ["account_name", "code"]
  }
  ```

---

## 7. Machine Contract SPI (`twofa.Provider`)

Registered under service name `"twofa.provider"` via `ctx.Provide`:

```go
type Provider interface {
    GetCode(ctx context.Context, orgID uint, accountName string) (code string, remainingSeconds int, err error)
    VerifyCode(ctx context.Context, orgID uint, accountName string, code string) (bool, error)
}
```

Allows other plugins (e.g. automated webhook executors, runner bots) to retrieve or verify 2FA codes without HTTP roundtrips.
