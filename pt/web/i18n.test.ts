import { describe, expect, it } from "vitest";
import { ptI18n } from "./i18n";

// Core's i18n:audit does not scan octarq-plugins, so this file is the only guard
// that keeps the five languages in step. A missing key falls back to English at
// runtime and nobody notices; a missing placeholder breaks a sentence.

const LANGS = ["en", "zh", "es", "pt", "ja"] as const;

function flatten(value: Record<string, unknown>, prefix = ""): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, entry] of Object.entries(value)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (entry && typeof entry === "object") {
      Object.assign(out, flatten(entry as Record<string, unknown>, path));
    } else if (typeof entry === "string") {
      out[path] = entry;
    }
  }
  return out;
}

function placeholders(value: string): string[] {
  return [...value.matchAll(/\{\{(\w+)\}\}/g)].map((match) => match[1]).sort();
}

describe("pt plugin i18n", () => {
  const english = flatten(ptI18n.en as Record<string, unknown>);
  const englishKeys = Object.keys(english).sort();

  it.each(LANGS)("%s has exactly the English key set", (lang) => {
    const dict = flatten(ptI18n[lang] as Record<string, unknown>);
    expect(Object.keys(dict).sort()).toEqual(englishKeys);
  });

  it.each(LANGS)("%s keeps every interpolation placeholder", (lang) => {
    const dict = flatten(ptI18n[lang] as Record<string, unknown>);
    for (const [key, value] of Object.entries(english)) {
      expect(placeholders(dict[key]), `placeholders of ${lang}.${key}`).toEqual(placeholders(value));
    }
  });

  it("contributes the sidebar label and plugin description for every language", () => {
    for (const lang of LANGS) {
      const dict = flatten(ptI18n[lang] as Record<string, unknown>);
      expect(dict["_shared.nav.pt"]).toBeTruthy();
      expect(dict["_shared.settings.pluginDesc.pt"]).toBeTruthy();
    }
  });

  it("translates the sidebar label outside English", () => {
    for (const lang of ["zh", "es", "pt", "ja"] as const) {
      const dict = flatten(ptI18n[lang] as Record<string, unknown>);
      expect(dict["_shared.nav.pt"]).not.toBe("PT Accounts");
    }
  });

  it("keeps no empty strings", () => {
    for (const lang of LANGS) {
      const dict = flatten(ptI18n[lang] as Record<string, unknown>);
      for (const [key, value] of Object.entries(dict)) {
        expect(value.trim(), `${lang}.${key}`).not.toBe("");
      }
    }
  });
});
