# Octarq 2FA Vault Plugin — Engineering Specification

| Attribute | Value |
|---|---|
| **Plugin ID** | `twofa` |
| **Go Module** | `github.com/octarq-org/octarq-plugins/twofa` |
| **UI Package** | `@octarq/plugin-twofa` |
| **Tier Classification** | Tier 2 (Module Engineering Spec — not embedded in the Go binary) |
| **Version** | `1.1.0` |

---

## 1. Overview

The 2FA Vault plugin stores TOTP accounts for third-party services. It generates live codes and verifies submitted codes. It exposes MCP tools and a cross-plugin Provider for identity-less callers.

The plugin keeps one vault. Every account is personal by default. The owner can share an account with the active workspace. A shared account grants read access to live codes and verification. It never grants edit, delete, reveal, or export.

---

## 2. Storage Model

### 2.1 Vault items

The plugin stores no secrets in its own tables. Each account is a core VaultService item:

| Field | Value |
|---|---|
| `Scope` | `user` |
| `OrgID` | `Session().OrgID(r)` (the active workspace) |
| `OwnerUserID` | the caller |
| `ItemType` | `twofa_totp` |
| `CollectionSlug` | `default` |

Items are personal to their creator. Items are also scoped to the active workspace org. The same user in another workspace org has a separate list.

The core vault encrypts the secret payload. List responses never include plaintext secrets.

### 2.2 `twofa_shares`

The plugin owns one table for sharing:

```sql
CREATE TABLE twofa_shares (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id INTEGER NOT NULL,
    item_id INTEGER NOT NULL,
    shared_by INTEGER,
    created_at DATETIME,
    UNIQUE (org_id, item_id)
);

CREATE INDEX idx_twofa_shares_org_id ON twofa_shares(org_id);
```

A row grants every other member of `org_id` read access to `item_id`. The vault item keeps its owner. The owner stays the only writer.

The plugin registers the model in `Models()`. `Mount` copies `ctx.DB` into `p.db`.

---

## 3. Permission Matrix

Owner means the creator of the item. Shared member means another member of the same active org with a share row. Other member means a member with no share row.

| Operation | Owner | Shared member | Other member |
|---|---|---|---|
| List accounts | own items | shared items (`mine=false`) | own items |
| Create account | allowed | allowed | allowed |
| Get account / reveal | allowed | 404 | 404 |
| Update account | allowed | 404 | 404 |
| Delete account | allowed | 404 | 404 |
| Pin toggle | allowed | 404 | 404 |
| Share toggle | allowed | 404 | 404 |
| Generate code | allowed | allowed | 404 |
| Verify code | allowed | allowed | 404 |
| Show QR code | allowed | 404 | 404 |
| Import | own vault | own vault | own vault |
| Export | own items | own items | own items |

The plugin does not grant an admin override. Ownership is the only write boundary.

### 3.1 List semantics

The list contains two groups:

1. The caller's own user-scoped `twofa_totp` items.
2. Every share row in the active org whose item belongs to another owner.

Each summary carries `mine` and `shared`.

For the second group, the plugin loads each item. It skips and deletes a share row when the item is gone. It also skips and deletes a row when the item is not `twofa_totp`, not in the active org, or not user-scoped.

### 3.2 Delete semantics

`DELETE /api/twofa/accounts/{id}` deletes the vault item. It then deletes the `twofa_shares` row.

---

## 4. REST API

All routes are under one group, `/api/twofa`, and pass through `ctx.Guard`.

| Method | Path | Access | Audit action |
|---|---|---|---|
| `GET` | `/api/twofa/accounts` | member | — |
| `POST` | `/api/twofa/accounts` | member | `create_account` |
| `GET` | `/api/twofa/accounts/{id}` | owner | `view_secret` when `?reveal=true` |
| `PUT` | `/api/twofa/accounts/{id}` | owner | `update_account` |
| `DELETE` | `/api/twofa/accounts/{id}` | owner | `delete_account` |
| `POST` | `/api/twofa/accounts/{id}/pin` | owner | — |
| `POST` | `/api/twofa/accounts/{id}/share` | owner | `share_account` / `unshare_account` |
| `POST` | `/api/twofa/accounts/{id}/code` | owner or shared | `generate_code` |
| `POST` | `/api/twofa/accounts/{id}/verify` | owner or shared | `verify_code` |
| `GET` | `/api/twofa/accounts/{id}/qr` | owner | `view_qr` |
| `POST` | `/api/twofa/import` | member | `import_accounts` |
| `GET` | `/api/twofa/export` | member | `export_accounts` |

The share endpoint takes `{"shared": true|false}` and returns `{"shared": bool}`. A repeated share request does not create a duplicate row.

---

## 5. MCP Tools

MCP callers carry no user identity. Every tool operates on shared items in the caller's org only. Personal items never resolve.

| Tool | Behaviour |
|---|---|
| `get_2fa_code(account_name)` | Returns the live code and seconds remaining for a shared account |
| `list_2fa_accounts(tag)` | Lists shared accounts. Never returns secrets or codes |
| `verify_2fa_code(account_name, code)` | Reports whether the code is valid for a shared account |

Name matching is case-insensitive and matches either the account name or the issuer.

---

## 6. Cross-Plugin Provider

The plugin registers `twofa.Provider` under `twofa.provider`.

```go
type Provider interface {
    GetCode(ctx context.Context, orgID uint, accountName string) (code string, remainingSeconds int, err error)
    VerifyCode(ctx context.Context, orgID uint, accountName string, code string) (bool, error)
}
```

The Provider has no user identity. It resolves shared items in `orgID` only. It never resolves a personal item.

---

## 7. Menu and Route

The plugin declares one menu item:

| Field | Value |
|---|---|
| ID | `twofa` |
| Label | `2FA Vault` |
| Path | `/personal/twofa` |
| Icon | `shield` |
| Category | `Accounts` |
| Area | `personal` |
| Order | 20 |

The frontend registers one route, `/personal/twofa`. There is no governance area and no team page.

---

## 8. Failure Modes

The plugin fails closed.

| Condition | Response |
|---|---|
| No session org or no caller user | `401 unauthorized workspace` |
| Vault service unavailable | `500` |
| `p.db == nil` on list, share, delete, or summary reads | `500 share store unavailable` |
| Item missing, foreign owner, wrong type, wrong org, or not shared | `404 account not found` |
| Invalid request body or base32 secret | `400` |
| MCP or Provider called with no org | error |
| MCP or Provider called with `p.db == nil` | error |

A `404` is the same for "absent" and "not yours". The response never reveals that another member's item exists.

---

## 9. Audit Trail

The plugin calls `ctx.Audit` for these actions:

- `create_account`, `update_account`, `delete_account`
- `view_secret`, `view_qr`
- `generate_code`, `verify_code`
- `share_account`, `unshare_account`
- `import_accounts`, `export_accounts`
- `mcp_get_code`, `mcp_verify_code`

Each entry records the actor, the account name, the org, the source address, and the time.

---

## 10. Non-Functional Requirements

- Codes follow RFC 6238 with drift tolerance of one step.
- Verification uses constant-time comparison.
- Share insertion is idempotent through the `(org_id, item_id)` unique index.
- Listing deletes stale share rows instead of returning empty entries.
