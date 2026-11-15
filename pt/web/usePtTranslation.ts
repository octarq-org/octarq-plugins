import { useCallback } from "react";
import { useTranslation } from "@octarq/plugin-sdk";
import type { Lang, TFunc } from "@octarq/plugin-sdk";

// usePtTranslation scopes t() to this plugin's namespace. Composed plugin
// dictionaries are filed under the plugin name, so a bare key never resolves
// and every lookup would fall back to its English default.
export function usePtTranslation(): { t: TFunc; lang: Lang } {
  const { t, lang } = useTranslation();
  const scoped = useCallback<TFunc>(
    (key, fallbackOrVars, vars) => t(`pt.${key}`, fallbackOrVars, vars),
    [t],
  );
  return { t: scoped, lang };
}
