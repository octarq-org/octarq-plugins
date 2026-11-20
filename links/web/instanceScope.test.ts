import { describe, it, expect } from "vitest";

const SOURCES = import.meta.glob("./{index.ts,pages/index.tsx}", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const TENANT_PAGE = "./pages/index.tsx";
const PLUGIN_ENTRY = "./index.ts";

function sourceFor(key: string): string {
  const hit = Object.entries(SOURCES).find(([k]) => k === key);
  if (!hit) throw new Error(`import.meta.glob resolved no source at "${key}"`);
  return hit[1];
}

describe("links instance scope", () => {
  it("keeps instance-scope settings out of the tenant Links page", () => {
    const page = sourceFor(TENANT_PAGE);

    expect(page).not.toMatch(/\bLinkSettings\b/);
    expect(page).not.toMatch(/tab\s*===\s*'settings'/);
    expect(page).not.toMatch(/setTab\s*\(\s*'settings'/);
  });

  it("does not register duplicate instance link settings route", () => {
    const entry = sourceFor(PLUGIN_ENTRY);

    expect(entry).not.toMatch(/path:\s*"\/link-settings"/);
  });
});