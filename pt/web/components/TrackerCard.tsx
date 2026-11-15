import { Badge, Button } from "@octarq/plugin-sdk";
import type { TFunc } from "@octarq/plugin-sdk";
import {
  AlertTriangle,
  Clock,
  Gauge,
  Info,
  LogIn,
  Pencil,
  RefreshCw,
  Sparkles,
  Trash2,
} from "lucide-react";
import type { Tracker } from "../types";
import { formatBytes, formatDateTime, formatRatio, outcomeKey, outcomeTone } from "../format";

// TrackerCard is one account: what the site says, when the plugin runs next,
// and the actions the site type actually supports. A site without a public
// sign-in endpoint never shows a sign-in button.
export interface TrackerCardProps {
  tracker: Tracker;
  t: TFunc;
  lang: string;
  busy: boolean;
  onSignin: () => void;
  onLogin: () => void;
  onRefresh: () => void;
  onDetails: () => void;
  onEdit: () => void;
  onDelete: () => void;
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="truncate text-[13.5px] font-medium text-foreground">{value}</div>
    </div>
  );
}

export function TrackerCard({
  tracker,
  t,
  lang,
  busy,
  onSignin,
  onLogin,
  onRefresh,
  onDetails,
  onEdit,
  onDelete,
}: TrackerCardProps) {
  const snap = tracker.latest_snapshot;
  const unknown = t("unknown", "—");
  const signinLabel = tracker.last_signin_outcome
    ? t(outcomeKey(tracker.last_signin_outcome), tracker.last_signin_outcome)
    : t("pending", "Waiting");

  return (
    <article className="flex h-full flex-col rounded-2xl border border-border bg-card p-5 shadow-[var(--shadow-card)]">
      <header className="mb-3 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="truncate font-display text-[1.0625rem] font-semibold tracking-[-0.01em] text-foreground">
            {tracker.name}
          </h3>
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            <Badge tone="secondary">{tracker.schema_label || tracker.schema}</Badge>
            {tracker.enabled ? null : <Badge tone="default">{t("disabled", "Paused")}</Badge>}
            {tracker.cookie_valid ? (
              <Badge tone="success" shape="dot">
                {t("cookieValid", "Login valid")}
              </Badge>
            ) : (
              <Badge tone="danger" shape="dot">
                {t("cookieInvalid", "Login expired")}
              </Badge>
            )}
            {tracker.last_signin_outcome ? (
              <Badge tone={outcomeTone(tracker.last_signin_outcome)} shape="square">
                {signinLabel}
              </Badge>
            ) : null}
          </div>
        </div>
        <Button variant="ghost" size="sm" onClick={onEdit} aria-label={t("edit", "Edit")}>
          <Pencil className="h-4 w-4" />
        </Button>
      </header>

      {tracker.needs_manual_action ? (
        <div className="mb-3 flex items-start gap-2 rounded-lg border border-warning-border bg-warning-bg px-3 py-2 text-[12.5px] leading-relaxed text-warning-fg">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <div className="min-w-0">
            <div className="font-semibold">{t("manualTitle", "A person must act")}</div>
            <div className="break-words">{tracker.last_error || tracker.manual_reason}</div>
          </div>
        </div>
      ) : null}

      <div className="mb-3 grid grid-cols-2 gap-3 sm:grid-cols-3">
        <Metric label={t("level", "Level")} value={snap?.user_level || unknown} />
        <Metric label={t("uploaded", "Upload")} value={formatBytes(snap?.uploaded_bytes, unknown)} />
        <Metric label={t("downloaded", "Download")} value={formatBytes(snap?.downloaded_bytes, unknown)} />
        <Metric
          label={t("ratio", "Ratio")}
          value={formatRatio(snap?.ratio, snap?.ratio_infinite ?? false, unknown)}
        />
        <Metric
          label={t("bonus", "Bonus")}
          value={snap?.bonus === undefined ? unknown : Number(snap.bonus).toLocaleString()}
        />
        <Metric
          label={t("warnings", "Warnings")}
          value={snap?.warning_count === undefined ? unknown : String(snap.warning_count)}
        />
      </div>

      <dl className="mb-4 space-y-1 text-[12px] text-muted-foreground">
        <div className="flex items-center gap-1.5">
          <Clock className="h-3.5 w-3.5" />
          <dt>{t("lastSignin", "Last sign-in")}:</dt>
          <dd className="truncate">{formatDateTime(tracker.last_signin_at, t("never", "Never"), lang)}</dd>
        </div>
        <div className="flex items-center gap-1.5">
          <Gauge className="h-3.5 w-3.5" />
          <dt>{t("nextSignin", "Next sign-in")}:</dt>
          <dd className="truncate">{formatDateTime(tracker.next_signin_at, t("never", "Never"), lang)}</dd>
        </div>
      </dl>

      {tracker.schema && !tracker.capabilities.signin ? (
        <p className="mb-3 text-[12px] leading-relaxed text-muted-foreground">
          {t("signinNotSupported", "This site has no public sign-in endpoint.")}
        </p>
      ) : null}

      <div className="mt-auto flex flex-wrap items-center gap-2">
        {tracker.capabilities.signin && tracker.signin_enabled ? (
          <Button size="sm" onClick={onSignin} disabled={busy}>
            <Sparkles className="mr-1.5 h-3.5 w-3.5" />
            {t("signinNow", "Sign in now")}
          </Button>
        ) : null}
        {tracker.capabilities.login ? (
          <Button size="sm" variant="secondary" onClick={onLogin} disabled={busy}>
            <LogIn className="mr-1.5 h-3.5 w-3.5" />
            {t("loginNow", "Sign in again")}
          </Button>
        ) : null}
        <Button size="sm" variant="secondary" onClick={onRefresh} disabled={busy}>
          <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          {t("refreshNow", "Refresh data")}
        </Button>
        <Button size="sm" variant="ghost" onClick={onDetails}>
          <Info className="mr-1.5 h-3.5 w-3.5" />
          {t("details", "Details")}
        </Button>
        <Button
          size="sm"
          variant="danger"
          className="ml-auto"
          onClick={onDelete}
          aria-label={t("remove", "Delete")}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      </div>
    </article>
  );
}
