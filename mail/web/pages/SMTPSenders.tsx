import { Empty, Field, Input, Modal, Badge, StatusBadge, Button, PageSection, PageRows, Separator, Skeleton, toast, confirmDialog, FormError, useTranslation, roleSatisfies, useCurrentRole } from "@octarq/plugin-sdk";
import { Fragment, useEffect, useState } from "react";
import { NavLink } from "react-router-dom";
import { mailApi, SMTPSender } from "../api";
import { KeyRound, Send, Blocks } from "lucide-react";

// Which registry owns the sender rows. An instance with the Integrations Hub
// mounted uses hub credentials; a standalone Core instance has no hub and uses
// the local mail_smtp_senders table. The two registries number rows
// independently, so a page must read exactly one of them.
type SenderMode = "integration" | "local" | null;

export function SMTPSenders() {
  const { t } = useTranslation();
  const { role, isInstanceAdmin } = useCurrentRole();
  const canManageSMTP = roleSatisfies("admin", role, isInstanceAdmin);
  const [mode, setMode] = useState<SenderMode>(null);
  const [senders, setSenders] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<any | null>(null);
  const [testingId, setTestingId] = useState<number | null>(null);

  async function load() {
    setLoading(true);
    try {
      const res = await fetch("/api/integrations?provider_type=communication", {
        credentials: "same-origin",
      });
      if (res.status === 404) {
        setMode("local");
        setSenders(await mailApi.smtpSenders());
        return;
      }
      if (!res.ok) {
        setMode(null);
        setSenders([]);
        return;
      }
      const data = await res.json();
      setMode("integration");
      setSenders(
        (Array.isArray(data) ? data : []).filter(
          (item: any) => item.provider === "smtp" || item.provider === "resend"
        )
      );
    } catch {
      setMode(null);
      setSenders([]);
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => { load(); }, []);

  async function remove(id: number) {
    if (!(await confirmDialog(t("settings.confirmRemoveSmtp")))) return;
    try {
      await mailApi.deleteSMTPSender(id);
      load();
    } catch (e: any) {
      toast.error(e.message || t("settings.failedRemove"));
    }
  }

  async function test(sender: any) {
    setTestingId(sender.id);
    try {
      if (mode === "local") {
        await mailApi.testSMTPSender(sender.id);
        toast.success(t("settings.smtpTestSent", { to: sender.fromEmail }));
      } else {
        const res = await fetch(`/api/integrations/${sender.id}/test`, {
          method: "POST",
          credentials: "same-origin",
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok || data.ok === false) {
          throw new Error(data.detail || data.message || "Connection test failed");
        }
        toast.success(data.message || t("settings.smtpTestSent", { to: sender.name }));
      }
    } catch (e: any) {
      toast.error(t("settings.testFailed", { msg: e?.message || "unknown error" }));
    } finally {
      setTestingId(null);
    }
  }

  return (
    <PageSection
      className="mt-0"
      title={t("settings.smtpOutgoingGateways")}
      description={t("settings.smtpOutgoingGatewaysDesc")}
      action={
        canManageSMTP && mode === "local" ? (
          <Button variant="primary" className="text-xs py-1 px-2.5" onClick={() => setCreating(true)}>
            {t("settings.addSmtp")}
          </Button>
        ) : canManageSMTP && mode === "integration" ? (
          <NavLink to="/settings/integrations?category=communication">
            <Button variant="primary" className="text-xs py-1 px-2.5">
              {t("settings.openIntegrationsHub")}
            </Button>
          </NavLink>
        ) : null
      }
    >

      {mode === "integration" && (
        <div className="mt-3 flex items-center justify-between p-3 rounded-lg border border-primary/20 bg-accent-soft text-xs text-foreground">
          <div className="flex items-center gap-2">
            <Blocks className="h-4 w-4 text-accent-fg shrink-0" />
            <span>{t("settings.integrationsHubBanner")}</span>
          </div>
          <NavLink
            to="/settings/integrations?category=communication"
            className="ml-3 inline-flex min-h-8 items-center whitespace-nowrap text-xs font-medium text-accent-fg hover:underline"
          >
            {t("settings.openIntegrationsHub")} →
          </NavLink>
        </div>
      )}

      {loading ? (
        <div role="status" aria-busy="true" aria-label={t("settings.loadingLower")}>
          <PageRows>
            {Array.from({ length: 3 }).map((_, i) => (
              <Fragment key={i}>
                {i > 0 && <Separator />}
                <div
                  data-slot="page-row"
                  className="flex min-h-[var(--row-height,3.5rem)] items-center justify-between gap-4 px-4 py-3"
                >
                  <div className="flex-1 space-y-1.5">
                    <div className="flex items-center gap-2">
                      <Skeleton className="h-4 w-36" />
                      <Skeleton className="h-4 w-16 rounded-full" />
                    </div>
                    <Skeleton className="h-3 w-56" />
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Skeleton className="h-7 w-14 rounded-lg" />
                    <Skeleton className="h-7 w-20 rounded-lg" />
                  </div>
                </div>
              </Fragment>
            ))}
          </PageRows>
        </div>
      ) : senders.length === 0 ? (
        // The heading carries the add/open control, so the empty state states
        // the fact and adds no second button.
        <Empty reason={t("settings.noSmtpSenders")} className="mt-3">
          <Send />
        </Empty>
      ) : (
        <PageRows>
          {senders.map((s, idx) => (
            <Fragment key={s.id}>
              {idx > 0 && <Separator />}
              <div
                data-slot="page-row"
                className="flex min-h-[var(--row-height,3.5rem)] items-center justify-between gap-4 px-4 py-3 group"
              >
                <div className="min-w-0">
                  <div className="font-semibold text-sm text-foreground flex flex-wrap items-center gap-1.5">
                    {s.name}
                    {mode === "integration" ? (
                      <>
                        <Badge tone={s.provider === "resend" ? "secondary" : "info"} className="uppercase tracking-wider text-[10px]">
                          {s.provider}
                        </Badge>
                        {s.configured ?? true
                          ? <StatusBadge tone="success" className="text-[10px]"><KeyRound className="h-2.5 w-2.5 mr-0.5 inline" />{t("settings.smtpPasswordSet")}</StatusBadge>
                          : <StatusBadge tone="warning" className="text-[10px]">{t("settings.smtpNoPassword")}</StatusBadge>}
                      </>
                    ) : (
                      s.passSet
                        ? <StatusBadge tone="success" className="text-[10px]"><KeyRound className="h-2.5 w-2.5 mr-0.5 inline" />{t("settings.smtpPasswordSet")}</StatusBadge>
                        : <StatusBadge tone="warning" className="text-[10px]">{t("settings.smtpNoPassword")}</StatusBadge>
                    )}
                  </div>
                  <div className="text-xs text-muted-foreground mt-1 font-mono">
                    {mode === "integration"
                      ? (s.summary || `${s.fromEmail} via ${s.host}:${s.port || 587}`)
                      : `${s.fromEmail} via ${s.host}:${s.port}`}
                  </div>
                </div>
                {canManageSMTP && (
                  <div className="flex shrink-0 items-center gap-2">
                    <Button
                      variant="subtle"
                      onClick={() => test(s)}
                      disabled={testingId === s.id}
                      className="text-xs py-1 px-2.5"
                    >
                      {testingId === s.id ? t("settings.testing") : t("settings.test")}
                    </Button>
                    {mode === "integration" ? (
                      <NavLink to="/settings/integrations?category=communication">
                        <Button variant="subtle" className="text-xs py-1 px-2.5">
                          {t("settings.openIntegrationsHub")}
                        </Button>
                      </NavLink>
                    ) : (
                      <>
                        <Button variant="subtle" onClick={() => setEditing(s)} className="text-xs py-1 px-2.5">
                          {t("settings.editSmtp")}
                        </Button>
                        <Button
                          variant="danger"
                          onClick={() => remove(s.id)}
                          className="text-xs py-1 px-2.5 border-0"
                        >
                          {t("settings.remove")}
                        </Button>
                      </>
                    )}
                  </div>
                )}
              </div>
            </Fragment>
          ))}
        </PageRows>
      )}
      {(creating || editing) && (
        <SMTPSenderModal
          sender={editing}
          onClose={() => { setCreating(false); setEditing(null); }}
          onSaved={() => { setCreating(false); setEditing(null); load(); }}
        />
      )}
    </PageSection>
  );
}

function SMTPSenderModal({ sender, onClose, onSaved }: { sender: any; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState(sender?.name || "");
  const [host, setHost] = useState(sender?.host || "");
  const [port, setPort] = useState(sender?.port?.toString() || "");
  const [user, setUser] = useState(sender?.user || "");
  const [pass, setPass] = useState("");
  const [fromEmail, setFromEmail] = useState(sender?.fromEmail || "");
  const [err, setErr] = useState<string | { message?: string; status?: number; requestId?: string }>("");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true); setErr("");
    try {
      const payload = { name, host, port: parseInt(port, 10), user, pass, fromEmail };
      if (sender) {
        await mailApi.updateSMTPSender(sender.id, payload);
      } else {
        await mailApi.createSMTPSender(payload);
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
    <Modal title={sender ? t("settings.modifySmtpRelay") : t("settings.configureSmtpRelay")} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label={t("settings.senderConnectionName")}>
          <Input value={name} onChange={(e: any) => setName(e.target.value)} placeholder={t("settings.senderConnectionPlaceholder")} required autoFocus />
        </Field>

        <div className="flex gap-4">
          <div className="flex-[3]">
            <Field label={t("settings.smtpHostLabel")}>
              <Input className="font-mono text-xs" value={host} onChange={(e: any) => setHost(e.target.value)} placeholder="smtp.mailgun.org" required />
            </Field>
          </div>
          <div className="flex-1">
            <Field label={t("settings.smtpPortLabel")}>
              <Input type="number" className="font-mono text-xs" value={port} onChange={(e: any) => setPort(e.target.value)} placeholder="587" required />
            </Field>
          </div>
        </div>

        <Field label={t("settings.smtpUsernameLabel")}>
          <Input className="font-mono text-xs" value={user} onChange={(e: any) => setUser(e.target.value)} placeholder={t("settings.smtpUsernamePlaceholder")} required />
        </Field>

        <Field label={t("settings.smtpPasswordLabel")} hint={sender ? t("settings.smtpPasswordHint") : ""}>
          <Input type="password" className="font-mono text-xs" value={pass} onChange={(e: any) => setPass(e.target.value)} placeholder="••••••••" required={!sender} />
        </Field>

        <Field label={t("settings.defaultFromAddress")} hint={t("settings.defaultFromHint")}>
          <Input className="font-mono text-xs" value={fromEmail} onChange={(e: any) => setFromEmail(e.target.value)} placeholder="noreply@domain.com" required />
        </Field>

        {err && <FormError err={err} />}
        <div className="flex justify-end gap-2.5 pt-4 border-t border-border">
          <Button type="button" variant="ghost" onClick={onClose}>{t("settings.cancel")}</Button>
          <Button type="submit" variant="primary" disabled={busy || !name.trim() || !host.trim() || !port || !user.trim()}>
            {busy ? t("settings.savingDots") : t("settings.saveRelay")}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
