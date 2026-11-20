import { useCallback } from "react";
import { useTranslation } from "@octarq/plugin-sdk";
import type { Lang, TFunc } from "@octarq/plugin-sdk";

export function usePtTranslation(): { t: TFunc; lang: Lang } {
  const { t, lang } = useTranslation();
  const scoped = useCallback<TFunc>(
    (key, fallbackOrVars, vars) => t(`pt.${key}`, fallbackOrVars, vars),
    [t],
  );
  return { t: scoped, lang };
}
