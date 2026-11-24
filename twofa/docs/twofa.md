---
title: 2FA Vault
description: Store TOTP accounts for third-party services, generate live codes, and share selected accounts with your workspace.
category: operations
order: 25
routes:
  - /personal/twofa
---

The **2FA Vault** plugin stores Time-based One-Time Password (TOTP) accounts for services like AWS, Cloudflare, GitHub, and your registrar. It generates live verification codes on demand.

Open **2FA Vault** in the sidebar at `/personal/twofa`.

:::note
The vault encrypts every secret before it writes to the database. List views never return plaintext secrets.
:::

## One vault, private by default

The 2FA Vault holds one list of accounts. Every account is private to the member who created it.

You own the accounts that you add. Only you can read the secret, edit the account, or delete it.

## Share an account with the workspace

You can share any account that you own. Sharing is per account, and the default is private.

1. Find the account card.
2. Turn on **Shared with workspace**.

Workspace members can now see the account and read its live codes.

To stop sharing, turn **Shared with workspace** off. The account becomes private again.

### What shared members can do

- They see the account in their vault list.
- They read the live code.
- They copy the code.
- They verify a code.
- They see a **Shared** badge on the card.

### What shared members cannot do

- They cannot reveal the secret or the QR code.
- They cannot edit the account.
- They cannot pin, delete, or export the account.
- They cannot turn sharing off. Only the owner controls sharing.

:::caution
Sharing gives every member of the workspace access to live codes. Share only the accounts that the team needs.
:::

## Add an account

1. Click **+ Add Account**.
2. Choose an input method:
   - **Manual Entry**: Enter the title, issuer, username, and Base32 secret.
   - **Key URI**: Paste the `otpauth://` link from the service. The vault reads the fields for you.
3. Add tags to group and filter accounts.
4. Click **Save Account**.

:::tip
Pin an account to keep it at the top of your list. Only the owner can pin an account.
:::

## Read and copy codes

Each card shows the current code. The card also shows the seconds left before the code expires.

Click the code or click **Copy** to copy it.

## Reveal a secret or show a QR code

Only the owner can reveal a secret. Use this action to enroll a backup authenticator.

1. Click the QR code action on the account card.
2. The modal shows the secret, the `otpauth://` URI, and a QR code.
3. Scan the QR code with your authenticator app.

Each reveal is recorded in the audit log.

## Import and export

**Import** adds accounts to your own vault. Paste one `otpauth://` URI per line.

**Export** creates a backup of your own accounts only. Shared accounts belong to another member and stay out of your export.

:::danger
The exported file contains plaintext secrets. Store it in a safe place. Never share it.
:::

## Use 2FA with AI agents

Octarq exposes three tools over the Model Context Protocol (MCP). Agents have no user identity. They can use shared accounts only.

- `get_2fa_code(account_name)`: Returns the live code and the seconds left.
- `list_2fa_accounts(tag)`: Lists shared accounts. It never returns secrets or codes.
- `verify_2fa_code(account_name, code)`: Reports whether a code is valid now.

An agent can log in to a service without you. Agents never see your private accounts.

## Audit trail

The vault writes an audit entry for these actions:

- Account creation, update, and deletion.
- Secret reveal and QR display.
- Code generation and verification.
- Sharing and unsharing.
- Import and export.

Each entry records the actor, the time, and the source address.
