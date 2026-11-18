import { Field, Input, Textarea, Switch, Button, PageColumn, PageSection, PageCard, PageRows, PageRow, Separator, Skeleton, toast, useTranslation } from "@octarq/plugin-sdk";
import { useCallback, useEffect, useState } from "react";
import { mailApi, MailSettingsData } from "../api";

const MASK = "••••••••";

export function MailSettings() {
  const { t } = useTranslation();
  const [s, setS] = useState<MailSettingsData | null>(null);
  const reload = useCallback(() => {
    mailApi.settings().then(setS).catch(() => {});
  }, []);
  useEffect(() => { reload(); }, [reload]);

  const [reservedMailboxes, setReservedMailboxes] = useState("");
  const [inboundToken, setInboundToken] = useState("");
  const [tokenLoaded, setTokenLoaded] = useState(false);
  const [revealed, setRevealed] = useState(false);
  const [copied, setCopied] = useState(false);
  const [catchAll, setCatchAll] = useState(false);
  const [autoWrap, setAutoWrap] = useState(false);
  const [busy, setBusy] = useState(false);

  const adminView = s?.inboundTokenSet !== undefined;

  // The raw token is never part of the settings dump — it is fetched explicitly
  // from the admin-only /api/settings/inbound-token endpoint and masked until
  // the admin asks to see it.
  const loadToken = useCallback(async () => {
    if (tokenLoaded) return;
    try {
      const r = await mailApi.inboundToken();
      setInboundToken(r.inboundToken);
      setTokenLoaded(true);
    } catch {
      // First-run org or a transient failure — keep the mask.
    }
  }, [tokenLoaded]);

  useEffect(() => { if (s) { setReservedMailboxes(s.reservedMailboxes || ""); setCatchAll(s.catchAll || false); setAutoWrap(s.autoWrapLinks || false); } }, [s]);

  useEffect(() => { if (adminView) void loadToken(); }, [adminView, loadToken]);

  async function save() {
    setBusy(true);
    try {
      await mailApi.updateSettings({
        reservedMailboxes,
        ...(tokenLoaded ? { inboundToken } : {}),
        catchAll,
        autoWrapLinks: autoWrap,
      });
      toast.success(t("settings.saved"));
      if (tokenLoaded) {
        // A save may have rotated the token (clearing it mints a fresh one);
        // refresh so the displayed value and URL stay accurate.
        const r = await mailApi.inboundToken();
        setInboundToken(r.inboundToken);
      }
    } catch (e: any) {
      toast.error(e?.message || t("settings.saveFailed", "Failed to save settings"));
    } finally { setBusy(false); }
  }

  async function reveal() {
    await loadToken();
    setRevealed((v) => !v);
  }

  async function copyToken() {
    if (!tokenLoaded) await loadToken();
    if (!inboundToken) return;
    await navigator.clipboard.writeText(inboundToken);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  if (!s) {
    return (
      <PageColumn className="pb-0">
        <div role="status" aria-busy="true" aria-label={t("settings.loadingLower")}>
          <PageSection className="mt-0" title={t("settings.inboundMailboxesSettings")}>
            <PageCard className="space-y-5">
              <div className="space-y-2">
                <Skeleton className="h-3.5 w-36" />
                <Skeleton className="h-16 w-full rounded-lg" />
              </div>
              <div className="space-y-2">
                <Skeleton className="h-3.5 w-40" />
                <Skeleton className="h-9 w-full rounded-lg" />
              </div>
              <div className="space-y-2">
                <Skeleton className="h-3.5 w-32" />
                <Skeleton className="h-9 w-full rounded-lg" />
              </div>
            </PageCard>
            <PageRows>
              <div data-slot="page-row" className="flex min-h-[var(--row-height,3.5rem)] items-center justify-between gap-4 px-4 py-3">
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-4 w-36" />
                  <Skeleton className="h-3 w-64" />
                </div>
                <Skeleton className="h-6 w-11 shrink-0 rounded-full" />
              </div>
              <Separator />
              <div data-slot="page-row" className="flex min-h-[var(--row-height,3.5rem)] items-center justify-between gap-4 px-4 py-3">
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-3 w-56" />
                </div>
                <Skeleton className="h-6 w-11 shrink-0 rounded-full" />
              </div>
            </PageRows>
          </PageSection>
        </div>
      </PageColumn>
    );
  }

  const displayed = tokenLoaded && revealed ? inboundToken : MASK;

  return (
    <PageColumn className="pb-0">
      <PageSection className="mt-0" title={t("settings.inboundMailboxesSettings")}>
        <PageCard className="space-y-5">
          <Field label={t("settings.reservedMailboxesLabel")} hint={t("settings.reservedMailboxesHint")}>
            <Textarea className="font-mono text-xs" rows={2} value={reservedMailboxes} onChange={(e) => setReservedMailboxes(e.target.value)} placeholder="admin&#10;postmaster" />
          </Field>
          {adminView && (
            <>
              <Field label={t("settings.inboundWebhookUrlLabel")} hint={t("settings.inboundWebhookUrlHint")}>
                <Input
                  readOnly
                  className="font-mono text-xs"
                  value={`${location.origin}/api/v1/webhook/${s?.orgSlug || ""}/email/inbound/${displayed}`}
                  onFocus={(e) => e.currentTarget.select()}
                />
              </Field>
              <Field label={t("settings.inboundTokenLabel")} hint={t("settings.inboundTokenHint")}>
                <div className="flex gap-2">
                  <Input
                    className="font-mono text-xs"
                    value={displayed}
                    readOnly={!revealed || !tokenLoaded}
                    onChange={(e) => setInboundToken(e.target.value)}
                    placeholder={t("settings.inboundTokenPlaceholder")}
                  />
                  <Button variant="outline" className="shrink-0 text-xs" onClick={reveal}>
                    {revealed ? t("settings.inboundTokenHide") : t("settings.inboundTokenReveal")}
                  </Button>
                  <Button variant="subtle" className="shrink-0 text-xs" onClick={copyToken}>
                    {copied ? t("uiCommon.copied") : t("settings.inboundTokenCopy")}
                  </Button>
                </div>
              </Field>
            </>
          )}
        </PageCard>
        <PageRows>
          <PageRow
            title={t("settings.enableCatchAll")}
            description={t("settings.enableCatchAllDesc")}
            control={<Switch checked={catchAll} onCheckedChange={setCatchAll} aria-label={t("settings.enableCatchAll")} />}
          />
          <Separator />
          <PageRow
            title={t("settings.autoWrapLinks")}
            description={t("settings.autoWrapLinksDesc")}
            control={<Switch checked={autoWrap} onCheckedChange={setAutoWrap} aria-label={t("settings.autoWrapLinks")} />}
          />
        </PageRows>
        <div className="mt-4 flex justify-end">
          <Button variant="primary" className="text-xs" onClick={save} disabled={busy}>{busy ? t("settings.saving") : t("settings.saveSettings")}</Button>
        </div>
      </PageSection>
    </PageColumn>
  );
}
