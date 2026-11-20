// @vitest-environment happy-dom
import { afterEach, describe, it, expect, vi, beforeEach } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@octarq/plugin-sdk";

vi.mock("@octarq/plugin-sdk", async (importOriginal) => {
  const ui = await importOriginal<typeof import("@octarq/plugin-sdk")>();
  return {
    ...ui,
    Empty: (props: { children?: React.ReactNode; reason?: React.ReactNode; detail?: React.ReactNode; action?: React.ReactNode }) => (
      <div data-testid="shared-empty">
        {props.children}
        {props.reason}
        {props.detail}
        {props.action}
      </div>
    ),
  };
});

vi.mock("../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api")>();
  return {
    ...actual,
    linksApi: {
      ...actual.linksApi,
      links: vi.fn().mockResolvedValue([]),
      domains: vi.fn().mockResolvedValue([]),
    },
    api: {
      ...actual.api,
      links: vi.fn().mockResolvedValue([]),
      domains: vi.fn().mockResolvedValue([]),
    },
  };
});

import LinksPage from "./index";

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(cleanup);

describe("LinksPage empty state", () => {
  it("renders the shared Empty when there are no links (not a handwritten copy)", async () => {
    render(
      <MemoryRouter initialEntries={["/links"]}>
        <I18nProvider>
          <LinksPage />
        </I18nProvider>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getAllByTestId("shared-empty").length).toBeGreaterThan(0);
    });
  });

  it("offers one create button while the list is empty", async () => {
    render(
      <MemoryRouter initialEntries={["/links"]}>
        <I18nProvider>
          <LinksPage />
        </I18nProvider>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getAllByTestId("shared-empty").length).toBeGreaterThan(0);
    });

    expect(screen.getAllByRole("button", { name: "links.newLink" })).toHaveLength(1);
  });
});
