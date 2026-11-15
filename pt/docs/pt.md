---
title: PT Accounts
description: Manage private-tracker accounts — refresh logins, sign in daily, and read traffic, ratio, bonus, and warnings.
category: operations
order: 40
---

# PT Accounts

Use this page to manage your private tracker (PT) accounts in one place. The plugin refreshes logins, signs in every day, and collects basic account data. It does not download torrents.

## Add a tracker

1. Open **PT Accounts**.
2. Select **Add tracker**.
3. Enter the site URL. Select the site type. Enter the account name and the account secret.
4. Select **Detect** to check that the site answers.
5. Select **Save**.

The plugin checks the cookie or API key before it saves anything. If the check fails, the plugin saves nothing. Correct the data, then save again.

:::tip
You can paste a browser cookie instead of the account data. This works for sites that need a browser challenge.
:::

## Refresh a login

Select **Log in again** on a tracker card. The plugin signs in to the site and stores the new session. A red badge means the stored session no longer works.

## Sign in for the day

Select **Sign in** on a tracker card. The plugin signs in to the site, then reports the result.

A sign-in has four possible results:

| Result | Meaning |
| --- | --- |
| Signed in | The site confirmed the sign-in. |
| Already signed in | The site reports that today's sign-in is done. This counts as success. |
| Not confirmed | The login works, but the site did not report a result. The plugin retries later. |
| Needs attention | A captcha or a browser check blocks the plugin. Paste a browser cookie to continue. |

:::caution
**Not confirmed** is not a success. The plugin retries it, then reports it. Check the site before you assume the sign-in worked.
:::

## Set the daily schedule

Each tracker has three schedule fields:

- **Sign-in window**: the hours when the plugin may sign in. The default window is any time.
- **Refresh interval**: how often the plugin reads account data. The default is 6 hours.
- **Minimum request interval**: the pause between two requests to the same site.

The plugin spreads the work across the window. It sends a few requests per day, not a burst.

## Read the account data

Each tracker card shows the current level, uploaded and downloaded traffic, share ratio, bonus points, and warnings. Select a card to see the traffic history and the sign-in history.

A blank value means the site did not report that value. A blank value is not zero.

## When a site needs a browser

Some sites show a captcha or a browser challenge. The plugin stops and asks for a cookie. It does not solve challenges.

1. Sign in to the site in your browser.
2. Copy the site cookie from the browser.
3. Select **Paste cookie** on the tracker card.

:::warning
The plugin does not support a proxy. If a site requires a proxy, this plugin cannot reach it.
:::

## Site rules

Automated sign-in may break the rules of some sites. You are responsible for the rules of each site you add.
