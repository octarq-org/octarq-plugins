// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { registerUIPlugin, resetRegistry, I18nProvider, RoleProvider } from "@octarq/plugin-sdk";
import linksPlugin from "../index";
import AbusePage from "./Abuse";
import { api } from "../api";

vi.mock("../api", () => ({
  api: {
    abuseReports: vi.fn(),
    updateAbuseReport: vi.fn(),
  },
  linksApi: {
    abuseReports: vi.fn(),
    updateAbuseReport: vi.fn(),
  },
}));

function renderPage() {
  return render(
    <RoleProvider value={{ role: "admin", isInstanceAdmin: true }}>
      <I18nProvider>
        <MemoryRouter>
          <AbusePage />
        </MemoryRouter>
      </I18nProvider>
    </RoleProvider>
  );
}

describe("AbusePage", () => {
  beforeEach(() => {
    cleanup();
    resetRegistry();
    registerUIPlugin(linksPlugin);
    vi.clearAllMocks();
  });

  it("renders empty state when there are no abuse reports", async () => {
    (api.abuseReports as any).mockResolvedValue([]);
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("No abuse reports. Reported links display here.")).toBeDefined();
    });
  });

  it("renders report card and details when reports exist", async () => {
    (api.abuseReports as any).mockResolvedValue([
      {
        id: 10,
        slug: "phish-link",
        target: "https://phishing.example.com",
        reason: "phishing",
        description: "Malicious credential harvesting link",
        reporterIp: "203.0.113.*",
        status: "open",
        createdAt: "2026-11-24T12:00:00Z",
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("/phish-link")).toBeDefined();
      expect(screen.getByText("https://phishing.example.com")).toBeDefined();
      expect(screen.getByText("Malicious credential harvesting link")).toBeDefined();
      expect(screen.getByText("IP: 203.0.113.*")).toBeDefined();
      expect(screen.getByText("Dismiss (safe)")).toBeDefined();
      expect(screen.getByText("Mark as reviewed")).toBeDefined();
    });
  });

  it("calls updateAbuseReport on dismiss and review button clicks", async () => {
    (api.abuseReports as any).mockResolvedValue([
      {
        id: 10,
        slug: "phish-link",
        target: "https://phishing.example.com",
        reason: "phishing",
        description: "Test description",
        reporterIp: "10.0.0.*",
        status: "open",
        createdAt: "2026-11-24T12:00:00Z",
      },
    ]);
    (api.updateAbuseReport as any).mockResolvedValue({ id: 10, status: "reviewed" });

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Mark as reviewed")).toBeDefined();
    });

    fireEvent.click(screen.getByText("Mark as reviewed"));

    await waitFor(() => {
      expect(api.updateAbuseReport).toHaveBeenCalledWith(10, "reviewed");
    });
  });
});
