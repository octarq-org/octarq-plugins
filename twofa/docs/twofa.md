---
title: 2FA Vault
description: Centralized TOTP two-factor authentication manager for shared accounts, with real-time verification codes and AI agent MCP access.
category: operations
order: 25
---

The **2FA Vault** plugin gives your team and AI agents a secure, centralized place to store Time-based One-Time Password (TOTP) credentials for third-party services like AWS, Cloudflare, GitHub, registrars, and cloud providers.

Rather than fragmenting authentication seeds across individual smartphones or browser extensions, the 2FA Vault keeps seeds encrypted at rest in your workspace while generating synchronized verification codes on demand.

:::note
All seeds and shared secrets stored in the 2FA Vault are encrypted using AES-256-GCM authenticated encryption before touching the database. Plaintext secrets are never returned in list endpoints.
:::

## Key Features

- **Real-Time Code Generation**: View 6-digit or 8-digit verification codes that automatically refresh every 30 seconds with animated countdown rings.
- **Agent-Native MCP Integration**: Connected AI agents (Cursor, Claude Code) can retrieve live codes via the `get_2fa_code` tool during automated operational tasks.
- **Key URI & QR Code Import**: Paste any standard `otpauth://totp/...` URI or scan QR codes to instantly configure new accounts with issuer, account, algorithm, and period.
- **Zero-Friction Sharing**: Share critical infrastructure access across teammates without needing to pass seeds around in chat apps or re-enrolling devices.
- **Comprehensive Audit Trail**: Every code generation, secret reveal, export, and MCP tool call is logged with timestamp, actor, and IP address.

## Managing 2FA Accounts

### Adding an Account

1. Navigate to **2FA Vault** in your workspace sidebar.
2. Click **+ Add Account**.
3. Choose your preferred input method:
   - **Manual Entry**: Enter the account title (e.g. `AWS Production`), issuer, username, and Base32 secret seed.
   - **Key URI (`otpauth://`)**: Paste the provisioning link provided by the third-party service. Octarq will automatically parse the issuer, account, secret, and parameters.
4. Add tags (e.g. `Cloud, Infrastructure`) for easy grouping and filtering.
5. Click **Save Account**.

:::tip
Pin frequently used credentials to keep them permanently fixed at the top of your 2FA dashboard for immediate access.
:::

### Viewing and Copying Verification Codes

Each credential card displays the current live numeric code formatted with a space (for example, `548 102`).
- Click anywhere on the code or click the **Copy** button to copy the code directly to your clipboard.
- The radial timer beside the code indicates how many seconds remain before the current code expires.

### Revealing Secrets & Displaying QR Codes

If you ever need to enroll a backup physical authenticator device:
1. Hover over the account card and click the **QR Code** action.
2. The modal displays the encrypted seed, the raw `otpauth://` URI, and a scannable QR code.
3. Inspecting or copying the secret creates an immutable record in your workspace audit log.

:::caution
Only workspace owners and authorized operators should reveal plaintext secret seeds. Always verify unfamiliar audit log entries in the audit trail.
:::

## Using 2FA with AI Agents (MCP)

Octarq exposes three dedicated tools to AI agents connected over the Model Context Protocol:

- `get_2fa_code(account_name)`: Returns the live TOTP code and remaining valid seconds.
- `list_2fa_accounts(tag)`: Lists registered account names and tags without revealing secret keys.
- `verify_2fa_code(account_name, code)`: Verifies whether an entered code is currently valid.

When an AI agent performs an operation such as logging into a cloud console or registering a domain, it can autonomously request the appropriate 2FA code without interrupting your workflow.
