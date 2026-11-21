import { Fragment, useMemo } from "react";
import { Button, Empty, PageCard, PageRows, Separator, Skeleton, Input, Badge, useTranslation } from "@octarq/plugin-sdk";
import { Domain, ProviderAccount } from "../api";
import { Globe, RefreshCw, Plus, Mail, Link as LinkIcon, Settings, Layers, ShieldCheck } from "lucide-react";
import type { DomainSubTab } from "./DomainDetailWorkspace";

export interface DomainListHubProps {
  domains: Domain[];
  accounts: ProviderAccount[];
  loading: boolean;
  q: string;
  onSearchChange: (q: string) => void;
  onSelectDomain: (d: Domain, subTab?: DomainSubTab) => void;
  onAddNewDomain: () => void;
  onSyncCloudflare: () => void;
  onConnectProvider: () => void;
  onToggleService: (domain: Domain, field: "forLink" | "forMail") => void;
  getProviderName: (dom: Domain) => string;
}

export function DomainListHub({
  domains,
  accounts,
  loading,
  q,
  onSearchChange,
  onSelectDomain,
  onAddNewDomain,
  onSyncCloudflare,
  onConnectProvider,
  onToggleService,
  getProviderName,
}: DomainListHubProps) {
  const { t } = useTranslation();

  const linkCount = useMemo(() => domains.filter((d) => d.forLink).length, [domains]);
  const mailCount = useMemo(() => domains.filter((d) => d.forMail).length, [domains]);

  if (domains.length === 0 && !loading) {
    return (
      <Empty
        reason={t("domains.addFirstDomain")}
        detail={
          <>
            <span className="block text-sm text-muted-foreground leading-relaxed">{t("domains.addFirstDomainHint")}</span>
            {accounts.length > 0 ? (
              <span className="mt-3 block text-xs text-muted-foreground">
                {t("domains.connectedProvidersPre")} <span className="font-mono tnum">{accounts.length}</span>
              </span>
            ) : (
              <span className="mt-3 block text-xs leading-relaxed text-muted-foreground">{t("domains.providerBlockerDetail")}</span>
            )}
          </>
        }
        action={
          <div className="flex flex-col items-center gap-2">
            {accounts.length > 0 ? (
              <Button variant="primary" onClick={onSyncCloudflare} className="gap-1.5">
                <RefreshCw className="h-4 w-4" />
                {t("domains.syncFrom", { name: accounts.length === 1 ? accounts[0].name : t("domains.provider") })}
              </Button>
            ) : (
              <Button variant="primary" onClick={onConnectProvider} className="gap-1.5">
                <Plus className="h-4 w-4" />
                {t("domains.connectProvider")}
              </Button>
            )}
            {accounts.length > 0 && (
              <button
                onClick={onAddNewDomain}
                className="inline-flex min-h-8 cursor-pointer items-center text-xs text-muted-foreground underline underline-offset-2 transition-colors hover:text-foreground"
              >
                {t("domains.orAddManually")}
              </button>
            )}
          </div>
        }
      >
        <div className="h-14 w-14 rounded-lg bg-accent-soft flex items-center justify-center text-accent-fg">
          <Globe className="h-7 w-7" />
        </div>
      </Empty>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2 flex-1 max-w-md">
          <Input
            className="text-sm"
            placeholder={t("domains.searchDomains")}
            value={q}
            onChange={(e) => onSearchChange(e.target.value)}
          />
          {q && (
            <Button variant="ghost" className="text-xs py-1.5" onClick={() => onSearchChange("")}>
              {t("domains.emptyFilteredAction")}
            </Button>
          )}
        </div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground font-medium">
          <span className="bg-well px-2.5 py-1 rounded-lg border border-border">
            {t("domains.allDomainsCount", { count: domains.length })}
          </span>
          <span className="bg-well px-2.5 py-1 rounded-lg border border-border text-accent-fg flex items-center gap-1">
            <LinkIcon className="h-3 w-3" />
            <span>{t("domains.linkEnabledCount", { count: linkCount })}</span>
          </span>
          <span className="bg-well px-2.5 py-1 rounded-lg border border-border text-success-fg flex items-center gap-1">
            <Mail className="h-3 w-3" />
            <span>{t("domains.mailEnabledCount", { count: mailCount })}</span>
          </span>
        </div>
      </div>

      {loading && domains.length === 0 ? (
        <div role="status" aria-busy="true" aria-label={t("domains.loading")}>
          <PageRows className="mt-0">
            {[0, 1, 2, 3, 4].map((i) => (
              <Fragment key={i}>
                {i > 0 && <Separator />}
                <div className="flex min-h-[var(--row-height,3.5rem)] flex-wrap items-center justify-between gap-4 px-4 py-3">
                  <div className="flex items-center gap-3 min-w-0">
                    <Skeleton className="h-10 w-10 rounded-lg shrink-0" />
                    <div className="space-y-1.5">
                      <div className="flex items-center gap-2">
                        <Skeleton className="h-4 w-36" />
                        <Skeleton className="h-4 w-20 rounded-full" />
                      </div>
                      <Skeleton className="h-3 w-48" />
                    </div>
                  </div>
                  <div className="flex items-center gap-3 shrink-0">
                    <Skeleton className="h-8 w-28 rounded-lg" />
                    <Skeleton className="h-8 w-24 rounded-lg" />
                    <Skeleton className="h-8 w-9 rounded-lg" />
                    <Skeleton className="h-8 w-9 rounded-lg" />
                  </div>
                </div>
              </Fragment>
            ))}
          </PageRows>
        </div>
      ) : domains.length === 0 && q ? (
        <PageCard className="mt-0 flex flex-col items-center gap-3 p-8 text-center">
          <p className="text-sm text-muted-foreground">
            {t("domains.emptyFilteredReason")} <span className="font-mono text-foreground">{`“${q}”`}</span>
          </p>
          <Button variant="ghost" className="text-xs py-1.5" onClick={() => onSearchChange("")}>
            {t("domains.emptyFilteredAction")}
          </Button>
        </PageCard>
      ) : (
        <PageRows className="mt-0">
          {domains.map((d, i) => (
            <Fragment key={d.id}>
              {i > 0 && <Separator />}
              <div
                role="button"
                tabIndex={0}
                className="min-h-[var(--row-height,3.5rem)] px-4 py-3 hover:bg-surface-hover transition-colors cursor-pointer group focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                onClick={() => {
                  onSelectDomain(d, "records");
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onSelectDomain(d, "records");
                  }
                }}
              >
                <div className="flex flex-wrap items-center justify-between gap-4">
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="h-10 w-10 rounded-lg bg-accent-soft flex items-center justify-center text-accent-fg shrink-0">
                      <Globe className="h-5 w-5" aria-hidden="true" />
                    </div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-mono font-bold text-base text-foreground truncate">{d.name}</span>
                        <Badge tone="default" className="text-[11px] font-mono shrink-0">
                          {getProviderName(d)}
                        </Badge>
                      </div>
                      {d.note && (
                        <div className="truncate text-xs text-muted-foreground mt-1">{d.note}</div>
                      )}
                    </div>
                  </div>

                  <div className="flex items-center gap-3 shrink-0">
                    <div className="flex items-center gap-2 bg-well px-3 py-1.5 rounded-lg border border-border">
                      <button
                        type="button"
                        className={`flex items-center gap-1 text-xs px-2 py-0.5 rounded-md transition-colors cursor-pointer ${
                          d.forLink
                            ? "bg-accent text-accent-fg font-medium"
                            : "text-muted-foreground hover:text-foreground"
                        }`}
                        title={t("domains.toggleLinkRouting")}
                        onClick={(e) => {
                          e.stopPropagation();
                          onToggleService(d, "forLink");
                        }}
                      >
                        <LinkIcon className="h-3 w-3" />
                        <span>{t("domains.thLink")}</span>
                      </button>
                      <button
                        type="button"
                        className={`flex items-center gap-1 text-xs px-2 py-0.5 rounded-md transition-colors cursor-pointer ${
                          d.forMail
                            ? "bg-success-bg text-success-fg border border-success-border font-medium"
                            : "text-muted-foreground hover:text-foreground"
                        }`}
                        title={t("domains.toggleMailRouting")}
                        onClick={(e) => {
                          e.stopPropagation();
                          onToggleService(d, "forMail");
                        }}
                      >
                        <Mail className="h-3 w-3" />
                        <span>{t("domains.thMail")}</span>
                      </button>
                    </div>

                    <Button
                      variant="primary"
                      className="text-xs py-1.5 px-3 gap-1.5"
                      onClick={(e) => {
                        e.stopPropagation();
                        onSelectDomain(d, "records");
                      }}
                    >
                      <Layers className="h-3.5 w-3.5" />
                      {t("domains.manageZone")}
                    </Button>
                    <Button
                      variant="subtle"
                      className="text-xs py-1.5 px-2.5"
                      title={t("domains.domainHealth")}
                      onClick={(e) => {
                        e.stopPropagation();
                        onSelectDomain(d, "verification");
                      }}
                    >
                      <ShieldCheck className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      variant="subtle"
                      className="text-xs py-1.5 px-2.5"
                      title={t("domains.domainSettings")}
                      onClick={(e) => {
                        e.stopPropagation();
                        onSelectDomain(d, "settings");
                      }}
                    >
                      <Settings className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              </div>
            </Fragment>
          ))}
          {loading && (
            <>
              <Separator />
              <div
                role="status"
                aria-busy="true"
                aria-label={t("domains.loading")}
                className="flex min-h-[var(--row-height,3.5rem)] items-center justify-between gap-4 px-4 py-3"
              >
                <div className="flex items-center gap-3">
                  <Skeleton className="h-10 w-10 rounded-lg shrink-0" />
                  <Skeleton className="h-4 w-36" />
                </div>
                <Skeleton className="h-8 w-32 rounded-lg" />
              </div>
            </>
          )}
        </PageRows>
      )}
    </div>
  );
}
