// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import {
  I18nProvider,
  ConfirmProvider,
  PasswordConfirmProvider,
  RoleProvider,
  registerUIPlugin,
  resetRegistry,
} from "@octarq/plugin-sdk";
import domainsPlugin from "../index";
import DomainsPage from "./index";
import { dnsApi, Domain } from "../api";

vi.mock("../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api")>();
  return {
    ...actual,
    dnsApi: {
      ...actual.dnsApi,
      domains: vi.fn(),
      providerAccounts: vi.fn(),
      ddnsTokens: vi.fn(),
      records: vi.fn(),
    },
  };
});

const sampleDomain: Domain = {
  id: 1,
  name: "example.com",
  providerAccountId: null,
  zoneId: "",
  forMail: false,
  forLink: false,
  linkHosts: [],
  mailHosts: [],
  createdAt: "2026-01-01T00:00:00Z",
};

const testResources = {
  en: {
    settings: {
      addProvider: "Add Provider",
      dnsApiAccounts: "DNS API Accounts",
      dnsApiAccountsDesc: "DNS provider API keys (Cloudflare/DNSPod) to verify domains.",
      integrationsHubBanner: "Manage third-party credentials and cloud provider integrations centrally.",
      openIntegrationsHub: "Integrations Hub",
      loadingLower: "Loading...",
      noDnsProviders: "No DNS providers configured yet.",
    },
  },
};

function renderDomainsPage(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <I18nProvider resources={testResources}>
        <RoleProvider value={{ role: "admin", isInstanceAdmin: false }}>
          <ConfirmProvider>
            <PasswordConfirmProvider>
              <DomainsPage />
            </PasswordConfirmProvider>
          </ConfirmProvider>
        </RoleProvider>
      </I18nProvider>
    </MemoryRouter>
  );
}

describe("DomainsPage PageShell action visibility", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    cleanup();
    resetRegistry();
    registerUIPlugin(domainsPlugin);
    vi.clearAllMocks();

    vi.mocked(dnsApi.domains).mockResolvedValue([sampleDomain]);
    vi.mocked(dnsApi.providerAccounts).mockResolvedValue([]);
    vi.mocked(dnsApi.ddnsTokens).mockResolvedValue([]);
    vi.mocked(dnsApi.records).mockResolvedValue([]);

    originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
      const rawUrl = typeof input === "string" ? input : input instanceof URL ? input.toString() : (input as Request).url;
      const urlObj = new URL(rawUrl, "http://localhost");
      if (urlObj.pathname === "/api/vault/items") {
        return {
          ok: false,
          status: 404,
          json: async () => ({ detail: "Not Found" }),
        } as unknown as Response;
      }
      return {
        ok: true,
        status: 200,
        json: async () => ({}),
      } as unknown as Response;
    }) as typeof globalThis.fetch;
  });

  afterEach(() => {
    cleanup();
    resetRegistry();
    globalThis.fetch = originalFetch;
  });

  it("renders Sync Cloudflare and Add Domain actions at /domains when active is null", async () => {
    renderDomainsPage("/domains");

    await waitFor(() => {
      expect(screen.getByText("example.com")).toBeTruthy();
    });

    expect(screen.getByRole("button", { name: "Sync Cloudflare" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add Domain" })).toBeTruthy();
  });

  it("does not render Sync Cloudflare or Add Domain actions at /domains?tab=ddns", async () => {
    renderDomainsPage("/domains?tab=ddns");

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "New DDNS Token" })).toBeTruthy();
    });

    expect(screen.queryByRole("button", { name: "Sync Cloudflare" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add Domain" })).toBeNull();
  });

  it("does not render Sync Cloudflare or Add Domain actions at /domains?tab=providers", async () => {
    renderDomainsPage("/domains?tab=providers");

    await waitFor(() => {
      expect(screen.getAllByRole("button", { name: "Add Provider" }).length).toBeGreaterThan(0);
    });

    expect(screen.queryByRole("button", { name: "Sync Cloudflare" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add Domain" })).toBeNull();
  });

  it("does not render Sync Cloudflare or Add Domain actions at /domains?create=1", async () => {
    renderDomainsPage("/domains?create=1");

    await waitFor(() => {
      expect(screen.getByText("Add Domain Zone")).toBeTruthy();
    });

    expect(screen.queryByRole("button", { name: "Sync Cloudflare" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add Domain" })).toBeNull();
  });

  it("hides Sync Cloudflare and Add Domain actions when a domain detail view is active", async () => {
    renderDomainsPage("/domains");

    await waitFor(() => {
      expect(screen.getByText("example.com")).toBeTruthy();
    });

    expect(screen.getByRole("button", { name: "Sync Cloudflare" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add Domain" })).toBeTruthy();

    fireEvent.click(screen.getByText("example.com"));

    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Sync Cloudflare" })).toBeNull();
      expect(screen.queryByRole("button", { name: "Add Domain" })).toBeNull();
    });
  });
});
