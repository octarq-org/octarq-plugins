// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider, registerUIPlugin, resetRegistry, toast } from "@octarq/plugin-sdk";
import linksPlugin from "../index";
import LinksPage from "./index";

function jsonResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    headers: { get: (name: string) => (name === "content-type" ? "application/json" : null) },
  };
}

describe("LinksPage CSV Export", () => {
  beforeEach(() => {
    cleanup();
    resetRegistry();
    registerUIPlugin(linksPlugin);
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("downloads links.csv from GET /api/links/export.csv when Export CSV is clicked", async () => {
    const csvContent = "ID,Host,Slug,Target,Title,Clicks,CreatedAt\n1,go.link,docs,https://example.com,Docs,5,2026-01-01T00:00:00Z\n";
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      const rawUrl = typeof input === "string" ? input : input instanceof URL ? input.toString() : (input as Request).url;
      const path = new URL(rawUrl, "http://localhost").pathname;
      if (method === "GET" && path === "/api/links") return jsonResponse([]);
      if (method === "GET" && path === "/api/domains") return jsonResponse([]);
      if (method === "GET" && path === "/api/links/export.csv") {
        return {
          ok: true,
          status: 200,
          blob: async () => new Blob([csvContent], { type: "text/csv" }),
          headers: { get: () => "text/csv" },
        };
      }
      throw new Error(`Unexpected fetch: ${method} ${rawUrl}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const createObjectURLSpy = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:links-csv");
    const revokeObjectURLSpy = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});

    render(
      <MemoryRouter initialEntries={["/links"]}>
        <I18nProvider>
          <LinksPage />
        </I18nProvider>
      </MemoryRouter>
    );

    const exportBtn = await screen.findByRole("button", { name: "Export CSV" });
    fireEvent.click(exportBtn);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/api/links/export.csv"),
        expect.objectContaining({ credentials: "same-origin" })
      );
    });
    await waitFor(() => {
      expect(createObjectURLSpy).toHaveBeenCalledTimes(1);
      expect(clickSpy).toHaveBeenCalledTimes(1);
      expect(revokeObjectURLSpy).toHaveBeenCalledWith("blob:links-csv");
    });
  });

  it("shows toast.error when CSV export fails", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      const rawUrl = typeof input === "string" ? input : input instanceof URL ? input.toString() : (input as Request).url;
      const path = new URL(rawUrl, "http://localhost").pathname;
      if (method === "GET" && path === "/api/links") return jsonResponse([]);
      if (method === "GET" && path === "/api/domains") return jsonResponse([]);
      if (method === "GET" && path === "/api/links/export.csv") {
        return { ok: false, status: 500, headers: { get: () => "application/json" } };
      }
      throw new Error(`Unexpected fetch: ${method} ${rawUrl}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const toastErrorSpy = vi.spyOn(toast, "error").mockImplementation(() => "" as any);

    render(
      <MemoryRouter initialEntries={["/links"]}>
        <I18nProvider>
          <LinksPage />
        </I18nProvider>
      </MemoryRouter>
    );

    const exportBtn = await screen.findByRole("button", { name: "Export CSV" });
    fireEvent.click(exportBtn);

    await waitFor(() => {
      expect(toastErrorSpy).toHaveBeenCalledWith("Action failed. Try again.");
    });
  });
});
