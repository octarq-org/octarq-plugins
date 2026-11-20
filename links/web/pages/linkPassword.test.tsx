// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach } from "vitest";
import { cleanup, render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { registerUIPlugin, resetRegistry, I18nProvider } from "@octarq/plugin-sdk";
import linksPlugin from "../index";
import LinksPage from "./index";
import { Link } from "../api";

const mockLinksWithPassword: Link[] = [
  {
    id: 1,
    host: "link.acme.com",
    slug: "pass-protected",
    title: "Protected Link",
    tags: "secret",
    target: "https://acme.com/secret",
    note: "",
    expiresAt: null,
    expiredUrl: "",
    clickLimit: 0,
    archived: false,
    enabled: true,
    clicks: 5,
    hasPassword: true,
    routingRules: [],
    createdAt: "2026-06-01T00:00:00Z",
  },
];

function jsonResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    headers: { get: (name: string) => (name === "content-type" ? "application/json" : null) },
  };
}

describe("Link password redaction and edit preservation", () => {
  let updatedPayload: any = null;

  beforeEach(() => {
    cleanup();
    resetRegistry();
    registerUIPlugin(linksPlugin);
    updatedPayload = null;

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      const rawUrl = typeof input === "string" ? input : input instanceof URL ? input.toString() : (input as Request).url;
      const urlObj = new URL(rawUrl, "http://localhost");
      const path = urlObj.pathname;
      if (method === "GET" && path === "/api/links") return jsonResponse(mockLinksWithPassword);
      if (method === "GET" && path === "/api/domains") return jsonResponse([]);
      if (method === "GET" && path === "/api/ai/assist/status") return jsonResponse({ configured: false, provider: "" });
      if (method === "GET" && path === "/api/links/capabilities") return jsonResponse({ geoReady: false, mode: "unavailable" });
      if (method === "PUT" && path === "/api/links/1") {
        updatedPayload = JSON.parse((init?.body as string) || "{}");
        return jsonResponse({ ...mockLinksWithPassword[0], ...updatedPayload });
      }
      throw new Error(`unexpected request in test: ${method} ${rawUrl}`);
    });
    vi.stubGlobal("fetch", fetchMock);
  });

  it("hides plaintext password in table, uses type=password with blank value on edit, and omits password when left blank", async () => {
    render(
      <MemoryRouter initialEntries={["/links"]}>
        <I18nProvider>
          <LinksPage />
        </I18nProvider>
      </MemoryRouter>
    );

    await waitFor(() => {
      const lockBadge = screen.getByTitle("Access protection password");
      expect(lockBadge).toBeDefined();
      expect(lockBadge.textContent?.trim()).toBe("");
    });

    const editBtn = screen.getByTitle("Edit link");
    fireEvent.click(editBtn);

    await waitFor(() => {
      expect(screen.getByText("Edit link — Protected Link")).toBeDefined();
    });

    expect(screen.getByText("Key set. Fill to overwrite, blank to keep")).toBeDefined();
    const passwordInput = screen.getByPlaceholderText("e.g. 123456") as HTMLInputElement;
    expect(passwordInput.type).toBe("password");
    expect(passwordInput.value).toBe("");

    fireEvent.click(screen.getByText("Save link"));

    await waitFor(() => {
      expect(updatedPayload).not.toBeNull();
    });
    expect("password" in updatedPayload).toBe(false);
  });

  it("sends new password when filled on edit", async () => {
    render(
      <MemoryRouter initialEntries={["/links"]}>
        <I18nProvider>
          <LinksPage />
        </I18nProvider>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByTitle("Edit link")).toBeDefined();
    });

    fireEvent.click(screen.getByTitle("Edit link"));
    await waitFor(() => {
      expect(screen.getByText("Edit link — Protected Link")).toBeDefined();
    });

    const passwordInput = screen.getByPlaceholderText("e.g. 123456") as HTMLInputElement;
    fireEvent.change(passwordInput, { target: { value: "NewSecret456" } });
    fireEvent.click(screen.getByText("Save link"));

    await waitFor(() => {
      expect(updatedPayload).not.toBeNull();
    });
    expect(updatedPayload.password).toBe("NewSecret456");
  });
});
