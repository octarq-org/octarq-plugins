import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ConfirmProvider, I18nProvider, ToastProvider } from "@octarq/plugin-sdk";
import type { ListResponse, Overview, Tracker } from "./types";
import PTPage from "./Page";

const trackers: Tracker[] = [
  {
    id: 1,
    created_at: "2026-11-25T00:00:00Z",
    updated_at: "2026-11-25T00:00:00Z",
    name: "高清时间",
    base_url: "https://hdtime.org",
    domain: "hdtime.org",
    schema: "nexusphp",
    enabled: true,
    signin_enabled: true,
    signin_window_start_min: 0,
    signin_window_end_min: 0,
    stats_interval_hours: 6,
    timeout_seconds: 20,
    limit_interval_seconds: 2,
    limit_count: 0,
    limit_seconds: 0,
    cookie_valid: true,
    needs_manual_action: false,
    consecutive_failures: 0,
    schema_label: "NexusPHP",
    capabilities: { login: true, cookie_auth: true, api_key_auth: false, signin: true, stats: true, bonus: true, warnings: true, hit_and_run: false, invites: true, captcha: false },
    credential: { has_login_secret: false, has_cookie: true, has_api_key: false, secret_version: 1 },
  },
  {
    id: 2,
    created_at: "2026-11-25T00:00:00Z",
    updated_at: "2026-11-25T00:00:00Z",
    name: "红豆饭",
    base_url: "https://hdfans.org",
    domain: "hdfans.org",
    schema: "nexusphp",
    enabled: true,
    signin_enabled: true,
    signin_window_start_min: 0,
    signin_window_end_min: 0,
    stats_interval_hours: 6,
    timeout_seconds: 20,
    limit_interval_seconds: 2,
    limit_count: 0,
    limit_seconds: 0,
    cookie_valid: false,
    needs_manual_action: true,
    manual_reason: "captcha",
    consecutive_failures: 1,
    schema_label: "NexusPHP",
    capabilities: { login: true, cookie_auth: true, api_key_auth: false, signin: true, stats: true, bonus: true, warnings: true, hit_and_run: false, invites: true, captcha: true },
    credential: { has_login_secret: false, has_cookie: true, has_api_key: false, secret_version: 1 },
  },
  {
    id: 3,
    created_at: "2026-11-25T00:00:00Z",
    updated_at: "2026-11-25T00:00:00Z",
    name: "海棠",
    base_url: "https://www.htpt.cc",
    domain: "htpt.cc",
    schema: "nexusphp",
    enabled: false,
    signin_enabled: true,
    signin_window_start_min: 0,
    signin_window_end_min: 0,
    stats_interval_hours: 6,
    timeout_seconds: 20,
    limit_interval_seconds: 2,
    limit_count: 0,
    limit_seconds: 0,
    cookie_valid: false,
    needs_manual_action: false,
    consecutive_failures: 0,
    schema_label: "NexusPHP",
    capabilities: { login: true, cookie_auth: true, api_key_auth: false, signin: true, stats: true, bonus: true, warnings: true, hit_and_run: false, invites: true, captcha: false },
    credential: { has_login_secret: false, has_cookie: true, has_api_key: false, secret_version: 1 },
  },
];

const overview: Overview = {
  summary: {
    tracker_count: 3,
    enabled_count: 2,
    cookie_invalid_count: 2,
    manual_action_count: 1,
    ratio_infinite: false,
    warning_count: 0,
  },
  schemas: [],
  scheduler_available: true,
  server_timezone: "Asia/Shanghai",
};

vi.mock("./api", () => ({
  ApiError: class ApiError extends Error { status = 0; },
  apiGet: vi.fn(async (path: string) =>
    path === "/api/pt/trackers"
      ? ({ trackers, summary: overview.summary } satisfies ListResponse)
      : overview,
  ),
  apiSend: vi.fn(async () => ({})),
}));

function renderPage() {
  return render(
    <I18nProvider>
      <ToastProvider>
        <ConfirmProvider>
          <PTPage />
        </ConfirmProvider>
      </ToastProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  cleanup();
});

describe("PT page list filters", () => {
  it("shows every site by default", async () => {
    renderPage();
    await waitFor(() => expect(screen.getAllByRole("article")).toHaveLength(3));
    expect(screen.getByText("3 of 3 sites")).toBeTruthy();
  });

  it("narrows the list by name or domain", async () => {
    renderPage();
    await waitFor(() => expect(screen.getAllByRole("article")).toHaveLength(3));
    fireEvent.change(screen.getByPlaceholderText("Filter by name or domain"), { target: { value: "hdfans" } });
    await waitFor(() => expect(screen.getAllByRole("article")).toHaveLength(1));
    expect(screen.getByText("红豆饭")).toBeTruthy();
    expect(screen.getByText("1 of 3 sites")).toBeTruthy();
  });

  it("shows an empty state when nothing matches, with a clear action", async () => {
    renderPage();
    await waitFor(() => expect(screen.getAllByRole("article")).toHaveLength(3));
    fireEvent.change(screen.getByPlaceholderText("Filter by name or domain"), { target: { value: "zzz" } });
    expect(await screen.findByText("No site matches the filter")).toBeTruthy();
    fireEvent.click(screen.getAllByText("Clear filters")[0]);
    await waitFor(() => expect(screen.getAllByRole("article")).toHaveLength(3));
  });
});

describe("PT page site link", () => {
  it("links each card to the site in a new tab", async () => {
    renderPage();
    await waitFor(() => expect(screen.getAllByRole("article")).toHaveLength(3));
    const links = screen.getAllByText("Open site").map((node) => node.closest("a"));
    expect(links.map((link) => link?.getAttribute("href"))).toEqual([
      "https://hdtime.org",
      "https://hdfans.org",
      "https://www.htpt.cc",
    ]);
    for (const link of links) {
      expect(link?.getAttribute("target")).toBe("_blank");
      expect(link?.getAttribute("rel")).toBe("noopener noreferrer");
    }
  });
});
