import { useState, useEffect } from "react";
import { Badge, Button, confirmDialog, PageColumn, Panel, Skeleton, TabBar, toast, useTranslation } from "@octarq/plugin-sdk";
import { dnsApi, Domain, ProviderAccount, DNSVerifyResult } from "../api";
import {
  Globe,
  Trash2,
  ArrowLeft,
  ShieldCheck,
  Link as LinkIcon,
  Settings,
  Layers,
  RefreshCw,
} from "lucide-react";
import {
  DnsHostRow,
  LinkHostRow,
  LinkHostGuide,
  calculateReputationScore,
  DnsReputationScoreCard,
  DnsFixAlertBanner,
  DnsTroubleshootingGuide,
} from "./dnsStatus";
import { EmailBlueprintPanel } from "./EmailBlueprintPanel";
import { DomainEditorForm } from "./DomainEditorForm";
import { DomainHostManager } from "./DomainHostManager";
import { RecordsView } from "./RecordsView";

export type DomainSubTab = "records" | "routing" | "verification" | "settings";

export interface DomainDetailWorkspaceProps {
  domain: Domain;
  accounts: ProviderAccount[];
  canDeleteDomain: boolean;
  initialSubTab?: DomainSubTab;
  onBack: () => void;
  onDeleted: () => void;
  onUpdated: (domain: Domain) => void;
  onReloadDomains: (reset?: boolean) => void;
  getProviderName: (dom: Domain) => string;
}

export function DomainDetailWorkspace({
  domain,
  accounts,
  canDeleteDomain,
  initialSubTab = "records",
  onBack,
  onDeleted,
  onUpdated,
  onReloadDomains,
  getProviderName,
}: DomainDetailWorkspaceProps) {
  const { t } = useTranslation();
  const [activeSubTab, setActiveSubTab] = useState<DomainSubTab>(initialSubTab);
  const [dnsStatus, setDnsStatus] = useState<DNSVerifyResult | null>(null);
  const [verifying, setVerifying] = useState(false);
  const [fixing, setFixing] = useState(false);
  const [showBlueprintModal, setShowBlueprintModal] = useState(false);

  useEffect(() => {
    setActiveSubTab(initialSubTab);
  }, [initialSubTab]);

  useEffect(() => {
    setDnsStatus(null);
  }, [domain.id]);

  async function verifyDns(targetDomain?: Domain) {
    const dom = targetDomain || domain;
    if (!dom) return;
    setVerifying(true);
    try {
      const res = await dnsApi.verifyDNS(dom.id);
      setDnsStatus(res);
    } catch (e: unknown) {
      const err = e as { message?: string };
      toast.error(err.message || t("domains.verifyFailed"));
    } finally {
      setVerifying(false);
    }
  }

  // Automatically trigger DNS health verification when entering the verification sub-tab
  useEffect(() => {
    if (activeSubTab === "verification" && dnsStatus === null && !verifying) {
      verifyDns(domain);
    }
  }, [activeSubTab, domain, dnsStatus, verifying]);

  async function handleOneClickFix(targetDomain?: Domain) {
    const dom = targetDomain || domain;
    if (!dom) return;
    setFixing(true);
    try {
      const res = await dnsApi.applyEmailBlueprint(dom.id);
      toast.success(t("domains.fixSuccessToast", { applied: res.applied, skipped: res.skipped }));
      await verifyDns(dom);
    } catch (e: unknown) {
      const err = e as { message?: string };
      toast.error(err.message || t("domains.fixFailedToast"));
    } finally {
      setFixing(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4 pb-2 border-b border-border">
        <div className="flex items-center gap-3">
          <Button variant="subtle" size="sm" onClick={onBack}>
            <ArrowLeft className="h-4 w-4" />
            {t("domains.backToDomains")}
          </Button>
          <div className="flex items-center gap-2">
            <Globe className="h-5 w-5 text-accent-fg" />
            <h2 className="font-mono text-xl font-bold text-foreground">{domain.name}</h2>
            <Badge tone="default" className="text-xs font-mono ml-1">
              {getProviderName(domain)}
            </Badge>
          </div>
        </div>

        <div className="flex items-center gap-2">
          {canDeleteDomain && (
            <Button
              variant="danger"
              onClick={async () => {
                if (await confirmDialog(t("domains.removeConfirm", { name: domain.name }))) {
                  await dnsApi.deleteDomain(domain.id);
                  onDeleted();
                }
              }}
              className="py-1 px-3 text-xs"
            >
              <Trash2 className="h-3.5 w-3.5 mr-1" />
              {t("domains.delete")}
            </Button>
          )}
        </div>
      </div>

      <TabBar
        className="mb-6 flex-wrap"
        value={activeSubTab}
        onValueChange={(v) => setActiveSubTab(v as DomainSubTab)}
        items={[
          { value: "records", label: t("domains.domainRecords"), icon: <Layers className="h-4 w-4" /> },
          { value: "routing", label: t("domains.domainRouting"), icon: <LinkIcon className="h-4 w-4" /> },
          { value: "verification", label: t("domains.domainHealth"), icon: <ShieldCheck className="h-4 w-4" /> },
          { value: "settings", label: t("domains.domainSettings"), icon: <Settings className="h-4 w-4" /> },
        ]}
      />

      {/* Sub-tab 1: DNS Records */}
      {activeSubTab === "records" && (
        <Panel title={t("domains.dnsRecords")} description={t("domains.pageDescription")}>
          <RecordsView domain={domain} />
        </Panel>
      )}

      {/* Sub-tab 2: Subdomain Routing */}
      {activeSubTab === "routing" && (
        <Panel title={t("domains.managedHosts")} description={t("domains.syncToggleHint")}>
          <DomainHostManager
            domain={domain}
            onReload={async (updatedDomain?: Domain) => {
              onReloadDomains(true);
              if (updatedDomain) {
                onUpdated(updatedDomain);
              } else {
                try {
                  const res = await dnsApi.domains({ q: domain.name, limit: 50, offset: 0 });
                  const updated = res.find((d) => d.id === domain.id);
                  if (updated) onUpdated(updated);
                } catch {
                  /* ignore reload error */
                }
              }
            }}
          />
        </Panel>
      )}

      {/* Sub-tab 3: Health & Verification */}
      {activeSubTab === "verification" && (() => {
        const repScore = dnsStatus ? calculateReputationScore(dnsStatus, domain.name) : null;
        return (
          <Panel
            title={t("domains.dnsSetupVerification")}
            description={t("domains.verificationHint")}
            action={
              <Button variant="primary" size="sm" onClick={() => verifyDns(domain)} disabled={verifying}>
                <RefreshCw className={`h-3.5 w-3.5 ${verifying ? "animate-spin" : ""}`} />
                {verifying ? t("domains.verifying") : t("domains.verifyDnsSetup")}
              </Button>
            }
          >

            {verifying && dnsStatus === null ? (
              <div role="status" aria-busy="true" aria-label={t("domains.verifying")} className="space-y-6 pt-2">
                <div className="rounded-lg bg-well border border-border p-5 space-y-4">
                  <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                    <div className="flex items-start gap-4 flex-1">
                      <Skeleton className="h-16 w-16 rounded-lg shrink-0" />
                      <div className="space-y-2 flex-1">
                        <div className="flex items-center gap-2">
                          <Skeleton className="h-4 w-32" />
                          <Skeleton className="h-4 w-16 rounded-full" />
                        </div>
                        <Skeleton className="h-3 w-full max-w-md" />
                      </div>
                    </div>
                    <Skeleton className="h-5 w-28 rounded-full shrink-0" />
                  </div>
                  <Skeleton className="h-2 w-full rounded-full" />
                </div>
                <div className="space-y-3">
                  <Skeleton className="h-3.5 w-24" />
                  <div className="rounded-lg bg-well border border-border p-3 space-y-2.5">
                    <div className="flex items-center gap-2 px-1">
                      <Skeleton className="h-3.5 w-3.5 rounded-full" />
                      <Skeleton className="h-3.5 w-36" />
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5">
                      {[0, 1, 2].map((col) => (
                        <div key={col} className="flex flex-col items-center gap-2 p-3 rounded-lg border border-border bg-card">
                          <Skeleton className="h-3 w-12" />
                          <Skeleton className="h-5 w-20 rounded-full" />
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </div>
            ) : dnsStatus === null ? (
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 pt-2">
                {(["SPF", "DKIM", "DMARC"] as const).map((label) => (
                  <div key={label} className="flex flex-col items-center p-4 rounded-lg bg-well border border-border">
                    <span className="text-xs uppercase font-bold text-muted-foreground tracking-wider">{t("domains.statusLabel", { label })}</span>
                    <div className="mt-2"><Badge tone="default">{t("domains.unknown")}</Badge></div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="space-y-6 pt-2">
                {/* 1. Deliverability & Reputation Score Card */}
                {repScore && <DnsReputationScoreCard repScore={repScore} />}

                {/* 2. Visual Guidance & One-Click Auto-Fix Action Banner */}
                {repScore && (
                  <DnsFixAlertBanner
                    repScore={repScore}
                    hasProvider={Boolean(domain.providerAccountId)}
                    fixing={fixing}
                    onOneClickFix={() => handleOneClickFix(domain)}
                    onOpenDetails={() => setShowBlueprintModal(true)}
                  />
                )}

                {/* 3. Mail hosts posture list */}
                <div className="space-y-3">
                  <span className="text-xs uppercase font-bold text-muted-foreground tracking-wider">{t("domains.mailHosts")}</span>
                  {(dnsStatus.hosts?.length
                    ? dnsStatus.hosts
                    : [{ host: domain.name, spf: dnsStatus.spf, dmarc: dnsStatus.dmarc, dkim: dnsStatus.dkim }]
                  ).map((host) => (
                    <DnsHostRow key={host.host} host={host} />
                  ))}
                </div>

                {/* 4. Actionable diagnostic troubleshooting suggestions */}
                {repScore && repScore.issues.length > 0 && (
                  <DnsTroubleshootingGuide issues={repScore.issues} />
                )}

                {/* 5. Short link hosts */}
                {!!dnsStatus.links?.length && (
                  <div className="space-y-3">
                    <span className="text-xs uppercase font-bold text-muted-foreground tracking-wider">{t("domains.shortLinkHosts")}</span>
                    {dnsStatus.links.map((lh) => (
                      <LinkHostRow key={lh.host} link={lh} />
                    ))}
                  </div>
                )}
              </div>
            )}
            <LinkHostGuide apex={domain.name} />
          </Panel>
        );
      })()}

      {/* Sub-tab 4: Domain Settings */}
      {activeSubTab === "settings" && (
        <PageColumn className="pb-0">
          <Panel title={t("domains.domainSettings")}>
            <DomainEditorForm
              key={domain.id}
              domain={domain}
              accounts={accounts}
              onCancel={onBack}
              onSaved={(d) => {
                if (d) onUpdated(d);
                onReloadDomains(true);
                toast.success(t("domains.saveBasicInfo"));
              }}
            />
          </Panel>
        </PageColumn>
      )}

      {showBlueprintModal && (
        <EmailBlueprintPanel
          domainId={domain.id}
          hasProvider={Boolean(domain.providerAccountId)}
          onClose={() => setShowBlueprintModal(false)}
          onApplied={() => {
            verifyDns(domain);
          }}
        />
      )}
    </div>
  );
}
