// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider, registerUIPlugin, resetRegistry } from "@octarq/plugin-sdk";
import mailPlugin from "../index";
import { MailSettings } from "./MailSettings";

function jsonResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    headers: { get: (name: string) => (name === "content-type" ? "application/json" : null) },
  };
}

describe("MailSettings inbound webhook URL", () => {
  beforeEach(() => {
    cleanup();
    resetRegistry();
    registerUIPlugin(mailPlugin);
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("displays /api/webhook/{orgSlug}/email/inbound/... instead of /api/v1/webhook/...", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const rawUrl = typeof input === "string" ? input : input instanceof URL ? input.toString() : (input as Request).url;
      const path = new URL(rawUrl, "http://localhost").pathname;
      if (path === "/api/settings") {
        return jsonResponse({
          orgSlug: "acme-corp",
          inboundTokenSet: true,
          reservedMailboxes: "admin",
          catchAll: true,
          autoWrapLinks: false,
        });
      }
      if (path === "/api/settings/inbound-token") {
        return jsonResponse({ inboundToken: "secret-tok-123" });
      }
      throw new Error(`Unexpected fetch: ${rawUrl}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(
      <I18nProvider>
        <MailSettings />
      </I18nProvider>
    );

    const input = (await screen.findByDisplayValue(/\/api\/webhook\/acme-corp\/email\/inbound\//)) as HTMLInputElement;
    expect(input.value).toContain("/api/webhook/acme-corp/email/inbound/");
    expect(input.value).not.toContain("/api/v1/webhook/");
  });
});
