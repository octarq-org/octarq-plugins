import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Button,
  Empty,
  PageShell,
  Skeleton,
  StatCard,
  useConfirm,
  useToast,
} from "@octarq/plugin-sdk";
import { AlertTriangle, HardDrive, Plus, RefreshCw } from "lucide-react";
import { ApiError, apiGet, apiSend } from "./api";
import type { ActionOutcome, ListResponse, Overview, Tracker } from "./types";
import { TrackerCard } from "./components/TrackerCard";
import { TrackerForm } from "./components/TrackerForm";
import { TrackerDetail } from "./components/TrackerDetail";
import { formatBytes, formatRatio, outcomeKey } from "./format";
import { usePtTranslation } from "./usePtTranslation";

type ActionKind = "signin" | "login" | "refresh";

export default function PTPage() {
  const { t, lang } = usePtTranslation();
  const toast = useToast();
  const confirm = useConfirm();

  const [trackers, setTrackers] = useState<Tracker[]>([]);
  const [overview, setOverview] = useState<Overview | null>(null);
  const [loading, setLoading] = useState(true);
  const [status, setStatus] = useState<number | null>(null);
  const [busyId, setBusyId] = useState<number | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Tracker | undefined>(undefined);
  const [detailId, setDetailId] = useState<number | null>(null);

  const load = useCallback(async () => {
    try {
      const [list, summary] = await Promise.all([
        apiGet<ListResponse>("/api/pt/trackers"),
        apiGet<Overview>("/api/pt/overview"),
      ]);
      setTrackers(list.trackers ?? []);
      setOverview(summary);
      setStatus(null);
    } catch (err) {
      setStatus(err instanceof ApiError ? err.status : 0);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const detail = useMemo(
    () => (detailId === null ? null : trackers.find((row) => row.id === detailId) ?? null),
    [detailId, trackers],
  );

  const runAction = async (tracker: Tracker, kind: ActionKind) => {
    setBusyId(tracker.id);
    try {
      const result = await apiSend<ActionOutcome>("POST", `/api/pt/trackers/${tracker.id}/${kind}`);
      const label = outcomeLabel(t, result.outcome);
      const message = result.message ? `${label}: ${result.message}` : label;
      if (result.outcome === "success" || result.outcome === "already_signed") {
        toast.success(message);
      } else if (result.outcome === "rate_limited" || result.outcome === "network_error") {
        toast.warning(message);
      } else {
        toast.error(message);
      }
      await load();
    } catch (err) {
      const code = err instanceof ApiError ? err.status : 0;
      toast.error(
        code === 403
          ? t("errorForbidden", "You do not have permission for this action.")
          : code === 404
            ? t("errorNotFound", "This site no longer exists.")
            : code === 429
              ? t("errorRateLimited", "The site needs a pause. Try again later.")
              : t("errorGeneric", "The action failed. Try again."),
      );
    } finally {
      setBusyId(null);
    }
  };

  const remove = async (tracker: Tracker) => {
    if (!(await confirm({ message: t("confirmDelete", "Delete this site and its history?"), danger: true }))) {
      return;
    }
    try {
      await apiSend<{ ok: boolean }>("DELETE", `/api/pt/trackers/${tracker.id}`);
      if (detailId === tracker.id) setDetailId(null);
      toast.success(t("remove", "Delete"));
      await load();
    } catch (err) {
      toast.error(
        err instanceof ApiError && err.status === 403
          ? t("errorForbidden", "You do not have permission for this action.")
          : t("errorGeneric", "The action failed. Try again."),
      );
    }
  };

  const unknown = t("unknown", "—");
  const summary = overview?.summary;

  const headerAction = (
    <div className="flex items-center gap-2">
      <Button variant="secondary" onClick={() => void load()} disabled={loading}>
        <RefreshCw className="mr-1.5 h-4 w-4" />
        {t("reload", "Reload")}
      </Button>
      <Button
        onClick={() => {
          setEditing(undefined);
          setFormOpen(true);
        }}
      >
        <Plus className="mr-1.5 h-4 w-4" />
        {t("addTracker", "Add site")}
      </Button>
    </div>
  );

  if (loading) {
    return (
      <PageShell
        width="full"
        title={t("pageTitle", "PT Accounts")}
        description={t("pageDescription", "Private-tracker accounts: logins, daily sign-in, and traffic statistics.")}
        eyebrow="PT"
        action={headerAction}
      >
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-64 w-full rounded-lg" />
          ))}
        </div>
      </PageShell>
    );
  }

  if (status !== null) {
    const forbidden = status === 403;
    return (
      <PageShell
        width="full"
        title={t("pageTitle", "PT Accounts")}
        description={t("pageDescription", "Private-tracker accounts: logins, daily sign-in, and traffic statistics.")}
        eyebrow="PT"
        action={headerAction}
      >
        <Empty
          reason={forbidden ? t("errorForbidden", "You do not have permission.") : t("loadFailedReason", "The accounts could not be loaded.")}
          action={
            forbidden ? undefined : (
              <Button variant="secondary" onClick={() => void load()}>
                {t("loadFailedAction", "Try again")}
              </Button>
            )
          }
        />
      </PageShell>
    );
  }

  return (
    <PageShell
      width="full"
      title={t("pageTitle", "PT Accounts")}
      description={t("pageDescription", "Private-tracker accounts: logins, daily sign-in, and traffic statistics.")}
      eyebrow="PT"
      action={headerAction}
    >

      {overview && !overview.scheduler_available ? (
        <div className="mb-4 flex items-start gap-2 rounded-lg border border-warning-border bg-warning-bg px-3 py-2 text-[12.5px] leading-relaxed text-warning-fg">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{t("schedulerOff", "The scheduler is off on this host.")}</span>
        </div>
      ) : null}

      <div className="mb-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
        <StatCard label={t("statTrackers", "Sites")} value={summary?.tracker_count ?? trackers.length} index={0} />
        <StatCard label={t("statUploaded", "Total upload")} value={formatBytes(summary?.uploaded_bytes, unknown)} index={1} />
        <StatCard label={t("statDownloaded", "Total download")} value={formatBytes(summary?.downloaded_bytes, unknown)} index={2} />
        <StatCard
          label={t("statRatio", "Overall ratio")}
          value={formatRatio(summary?.ratio, summary?.ratio_infinite ?? false, unknown)}
          index={3}
        />
        <StatCard
          label={t("statBonus", "Total bonus")}
          value={summary?.bonus === undefined ? unknown : Math.round(summary.bonus).toLocaleString()}
          index={4}
        />
        <StatCard label={t("statCookieInvalid", "Logins expired")} value={summary?.cookie_invalid_count ?? 0} index={5} />
      </div>

      {trackers.length === 0 ? (
        <Empty
          reason={t("emptyTitle", "No tracker accounts yet")}
          detail={t("emptyDesc", "Add a site to refresh its login, sign in daily, and read traffic.")}
          action={
            <Button
              onClick={() => {
                setEditing(undefined);
                setFormOpen(true);
              }}
            >
              <HardDrive className="mr-1.5 h-4 w-4" />
              {t("addTracker", "Add site")}
            </Button>
          }
        />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {trackers.map((tracker) => (
            <TrackerCard
              key={tracker.id}
              tracker={tracker}
              t={t}
              lang={lang}
              busy={busyId === tracker.id}
              onSignin={() => void runAction(tracker, "signin")}
              onLogin={() => void runAction(tracker, "login")}
              onRefresh={() => void runAction(tracker, "refresh")}
              onDetails={() => setDetailId(tracker.id)}
              onEdit={() => {
                setEditing(tracker);
                setFormOpen(true);
              }}
              onDelete={() => void remove(tracker)}
            />
          ))}
        </div>
      )}

      {formOpen ? (
        <TrackerForm
          schemas={overview?.schemas ?? []}
          tracker={editing}
          t={t}
          onClose={() => setFormOpen(false)}
          onSaved={() => {
            setFormOpen(false);
            void load();
          }}
        />
      ) : null}

      {detail ? (
        <TrackerDetail tracker={detail} t={t} lang={lang} onClose={() => setDetailId(null)} />
      ) : null}
    </PageShell>
  );
}

function outcomeLabel(t: (key: string, fallback?: string) => string, outcome: string): string {
  return t(outcomeKey(outcome), outcome);
}
