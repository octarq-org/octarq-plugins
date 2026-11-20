import { describe, it, expect } from "vitest";
import { parseDnsFilter, buildDnsFilterQuery } from "./filters";

describe("DNS Records url-backed filters pure functions", () => {
  it("1. parses query params correctly and defaults empty query to defaults", () => {
    const parsed = parseDnsFilter("?type=A&q=example");
    expect(parsed).toEqual({ type: "A", q: "example" });

    const defaultParsed = parseDnsFilter("");
    expect(defaultParsed).toEqual({ type: "", q: "" });

    const typeOnly = parseDnsFilter("?type=CNAME");
    expect(typeOnly).toEqual({ type: "CNAME", q: "" });

    const searchOnly = parseDnsFilter("?q=test");
    expect(searchOnly).toEqual({ type: "", q: "test" });
  });

  it("2. serializes filter values to query without including default values", () => {
    const queryDefault = buildDnsFilterQuery({ type: "", q: "" });
    expect(queryDefault.toString()).toBe("");

    const queryType = buildDnsFilterQuery({ type: "MX", q: "" });
    expect(queryType.toString()).toBe("type=MX");

    const querySearch = buildDnsFilterQuery({ type: "", q: "foo" });
    expect(querySearch.toString()).toBe("q=foo");

    const queryBoth = buildDnsFilterQuery({ type: "AAAA", q: "bar" });
    expect(queryBoth.toString()).toBe("type=AAAA&q=bar");
  });

  it("3. satisfies roundtrip consistency (parse -> serialize -> parse)", () => {
    const originalFilters = { type: "TXT", q: "verification" };
    const serialized = buildDnsFilterQuery(originalFilters);
    const reParsed = parseDnsFilter(serialized);
    expect(reParsed).toEqual(originalFilters);

    const defaultFilters = { type: "", q: "" };
    const serializedDefault = buildDnsFilterQuery(defaultFilters);
    const reParsedDefault = parseDnsFilter(serializedDefault);
    expect(reParsedDefault).toEqual(defaultFilters);
  });

  it("4. preserves existing query parameters", () => {
    const existing = new URLSearchParams("tab=records&id=123");
    const updated = buildDnsFilterQuery({ type: "A", q: "sub" }, existing);
    expect(updated.get("tab")).toBe("records");
    expect(updated.get("id")).toBe("123");
    expect(updated.get("type")).toBe("A");
    expect(updated.get("q")).toBe("sub");
  });
});
