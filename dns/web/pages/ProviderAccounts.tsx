import { Empty, Field, Input, Modal, Badge, StatusBadge, Button, Select, PageSection, PageRows, Separator, Skeleton, toast, confirmDialog, FormError, useTranslation, roleSatisfies, useCurrentRole } from "@octarq/plugin-sdk";
import { Fragment, useEffect, useState } from "react";
import { NavLink } from "react-router-dom";
import { dnsApi, ProviderAccount } from "../api";
import { Cloud, KeyRound, Blocks } from "lucide-react";

type AccountMode = "integration" | "local" | null;

export function ProviderAccounts({ embed }: { embed?: boolean }) {
  const { t } = useTranslation();
  const { role, isInstanceAdmin } = useCurrentRole();
  const canManageProviders = roleSatisfies("admin", role, isInstanceAdmin);
  const [mode, setMode] = useState<AccountMode>(null);
  const [accounts, setAccounts] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<any | null>(null);

  async function load() {
    setLoading(true);
    try {
      const res = await fetch("/api/vault/items?provider_type=dns", {
        credentials: "same-origin",
      });
      if (res.status === 404) {
        setMode("local");
        setAccounts(await dnsApi.providerAccounts());
        return;
      }
      if (!res.ok) {
        setMode(null);
        setAccounts([]);
        return;
      }
      const data = await res.json();
      const items = Array.isArray(data) ? data : (data?.body ?? []);
      setMode("integration");
      setAccounts(Array.isArray(items) ? items : []);
    } catch {
      setMode(null);
      setAccounts([]);
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => { load(); }, []);

  async function remove(id: number) {
    if (!(await confirmDialog(t("settings.confirmRemoveProvider")))) return;
    try {
      await dnsApi.deleteProviderAccount(id);
      load();
    } catch (e: any) {
      toast.error(e.message || t("settings.failedRemove"));
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div className="text-xs font-semibold text-muted-foreground">
          {t("settings.dnsApiAccounts")}
          <div className="text-[10px] text-muted-foreground font-normal mt-0.5">{t("settings.dnsApiAccountsDesc")}</div>
        </div>
        {canManageProviders && mode === "local" && (
          <Button variant="primary" className="text-xs py-1 px-2.5" onClick={() => setCreating(true)}>
            {t("settings.addProvider")}
          </Button>
        )}
        {canManageProviders && mode === "integration" && (
          <NavLink to="/settings/integrations?category=dns">
            <Button variant="primary" className="text-xs py-1 px-2.5">
              {t("settings.openIntegrationsHub")}
            </Button>
          </NavLink>
        )}
      </div>

      {mode === "integration" && (
        <div className="flex items-center justify-between p-3 rounded-lg border border-primary/20 bg-accent-soft text-xs text-foreground">
          <div className="flex items-center gap-2">
            <Blocks className="h-4 w-4 text-accent-fg shrink-0" />
            <span>{t("settings.integrationsHubBanner")}</span>
          </div>
          <NavLink
            to="/settings/integrations?category=dns"
            className="ml-3 inline-flex min-h-8 items-center whitespace-nowrap text-xs font-medium text-accent-fg hover:underline"
          >
            {t("settings.openIntegrationsHub")} →
          </NavLink>
        </div>
      )}

      {loading ? (
        <div role="status" aria-busy="true" aria-label={t("settings.loadingLower")}>
          <PageRows className="mt-0">
            {[0, 1, 2].map((i) => (
              <Fragment key={i}>
                {i > 0 && <Separator />}
                <div className="flex min-h-[var(--row-height,3.5rem)] items-center justify-between gap-4 px-4 py-3">
                  <div className="min-w-0 flex-1 space-y-2">
                    <Skeleton className="h-4 w-36" />
                    <div className="flex items-center gap-1.5">
                      <Skeleton className="h-4 w-20 rounded-full" />
                      <Skeleton className="h-4 w-24 rounded-full" />
                    </div>
                  </div>
                  <div className="flex items-center gap-2 shrink-0">
                    <Skeleton className="h-7 w-14" />
                    <Skeleton className="h-7 w-16" />
                  </div>
                </div>
              </Fragment>
            ))}
          </PageRows>
        </div>
      ) : mode === null ? (
        <Empty reason={t("settings.dnsProvidersUnavailable")}>
          <Cloud />
        </Empty>
      ) : accounts.length === 0 ? (
        <Empty
          reason={t("settings.noDnsProviders")}
          action={
            canManageProviders && mode === "local" ? (
              <Button variant="outline" size="sm" className="text-xs" onClick={() => setCreating(true)}>
                {t("settings.addProvider")}
              </Button>
            ) : canManageProviders && mode === "integration" ? (
              <NavLink to="/settings/integrations?category=dns">
                <Button variant="outline" size="sm" className="text-xs">
                  {t("settings.openIntegrationsHub")}
                </Button>
              </NavLink>
            ) : undefined
          }
        >
          <Cloud />
        </Empty>
      ) : (
        <PageSection className="mt-0">
          <PageRows className="mt-0">
            {accounts.map((a, i) => {
              const providerType = a.provider || a.type || "cloudflare";
              const isConfigured = a.configured ?? a.hasCredentials ?? true;
              return (
                <Fragment key={a.id}>
                  {i > 0 && <Separator />}
                  <div className="flex min-h-[var(--row-height,3.5rem)] items-center justify-between gap-4 px-4 py-3">
                    <div className="min-w-0">
                      <div className="font-semibold text-sm text-foreground">{a.name}</div>
                      <div className="text-xs text-muted-foreground mt-1 flex items-center gap-1.5 flex-wrap">
                        <Badge
                          tone={providerType === "cloudflare" || providerType === "dnspod" ? "info" : "default"}
                          className="uppercase tracking-wider text-[10px]"
                        >
                          {providerType}
                        </Badge>
                        {isConfigured ? (
                          <StatusBadge tone="success" className="text-[10px]">
                            <KeyRound className="h-2.5 w-2.5 inline" />
                            {t("settings.credentialsSet")}
                          </StatusBadge>
                        ) : (
                          <StatusBadge tone="warning" className="text-[10px]">
                            {t("settings.noCredentials")}
                          </StatusBadge>
                        )}
                      </div>
                    </div>
                    {canManageProviders && mode === "integration" && (
                      <NavLink to="/settings/integrations?category=dns">
                        <Button variant="subtle" className="text-xs py-1 px-2.5">
                          {t("settings.openIntegrationsHub")}
                        </Button>
                      </NavLink>
                    )}
                    {canManageProviders && mode === "local" && (
                      <div className="flex items-center gap-2 shrink-0">
                        <Button variant="subtle" onClick={() => setEditing(a)} className="text-xs py-1 px-2.5">
                          {t("settings.edit")}
                        </Button>
                        <Button
                          variant="danger"
                          onClick={() => remove(a.id)}
                          className="text-xs py-1 px-2.5 border-0"
                        >
                          {t("settings.remove")}
                        </Button>
                      </div>
                    )}
                  </div>
                </Fragment>
              );
            })}
          </PageRows>
        </PageSection>
      )}
      {(creating || editing) && (
        <ProviderAccountModal
          account={editing}
          onClose={() => { setCreating(false); setEditing(null); }}
          onSaved={() => { setCreating(false); setEditing(null); load(); }}
        />
      )}
    </div>
  );
}

function ProviderAccountModal({ account, onClose, onSaved }: { account: any; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState(account?.name || "");
  const [type, setType] = useState(account?.type || "cloudflare");
  const [config, setConfig] = useState<string>("");
  const [types, setTypes] = useState<string[]>([]);
  const [err, setErr] = useState<string | { message?: string; status?: number; requestId?: string }>("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    dnsApi.dnsProviders().then(setTypes).catch(() => setTypes(["cloudflare", "dnspod"]));
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true); setErr("");
    try {
      let cfgObj: any = {};
      if (config.trim()) {
        try { cfgObj = JSON.parse(config.trim()); }
        catch { cfgObj = type === "cloudflare" ? { apiToken: config.trim() } : { token: config.trim() }; }
      }
      if (account) {
        await dnsApi.updateProviderAccount(account.id, { name, type, config: cfgObj });
      } else {
        await dnsApi.createProviderAccount({ name, type, config: cfgObj });
      }
      toast.success(t("settings.saved"));
      onSaved();
    } catch (e: any) {
      setErr(e);
      toast.error(e?.message || t("settings.saveFailed", "Failed to save settings"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={account ? t("settings.editProviderAccount") : t("settings.registerProviderAccount")} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label={t("settings.providerLabelName")}>
          <Input value={name} onChange={(e: any) => setName(e.target.value)} placeholder={t("settings.providerLabelPlaceholder")} required autoFocus />
        </Field>
        {!account && (
          <Field label={t("settings.dnsProviderType")}>
            <Select
              value={type}
              onValueChange={setType}
              className="text-sm"
              options={types.map((ty) => ({ value: ty, label: <span className="capitalize">{ty}</span> }))}
            />
          </Field>
        )}
        <Field label={t("settings.apiKeysCredentials")} hint={account ? t("settings.apiKeysHintExisting") : t("settings.apiKeysHintNew")}>
          <Input className="font-mono text-xs" type="password" value={config} onChange={(e: any) => setConfig(e.target.value)} placeholder={account ? "••••••••" : t("settings.apiKeysPlaceholderNew")} required={!account} />
        </Field>
        {err && <FormError err={err} />}
        <div className="flex justify-end gap-2.5 pt-4 border-t border-border">
          <Button type="button" variant="ghost" onClick={onClose}>{t("settings.cancel")}</Button>
          <Button type="submit" variant="primary" disabled={busy || !name.trim()}>
            {busy ? t("settings.savingDots") : t("settings.saveConnection")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
