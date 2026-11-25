import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Empty,
  Input,
  LoadFailure,
  LockedFallback,
  PageEmpty,
  PageShell,
  Select,
  Skeleton,
  StatCard,
  useConfirm,
  useToast,
} from "@octarq/plugin-sdk";
import { AlertTriangle, CalendarCheck, HardDrive, Plus, RefreshCw } from "lucide-react";
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
  const [bulk, setBulk] = useState<"signin" | "refresh" | null>(null);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");

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

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return trackers.filter((tracker) => {
      if (statusFilter === "valid" && !tracker.cookie_valid) return false;
      if (statusFilter === "expired" && tracker.cookie_valid) return false;
      if (statusFilter === "manual" && !tracker.needs_manual_action) return false;
      if (statusFilter === "paused" && tracker.enabled) return false;
      if (needle === "") return true;
      return (
        tracker.name.toLowerCase().includes(needle) ||
        tracker.domain.toLowerCase().includes(needle)
      );
    });
  }, [trackers, query, statusFilter]);

  const filtering = query.trim() !== "" || statusFilter !== "all";

  const statusOptions = useMemo(
    () => [
      { value: "all", label: t("filterAll", "All sites") },
      { value: "valid", label: t("filterValid", "Login valid") },
      { value: "expired", label: t("filterExpired", "Login expired") },
      { value: "manual", label: t("filterManual", "Needs action") },
      { value: "paused", label: t("filterPaused", "Paused") },
    ],
    [t],
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

  const waitForTick = useCallback(async () => {
    const started = Date.now();
    let seenRunning = false;
    for (;;) {
      try {
        const jobs = await apiGet<{ name: string; status: string }[]>("/api/cron/jobs");
        const tick = jobs.find((job) => job.name === "pt.tick");
        if (tick?.status === "running") {
          seenRunning = true;
        } else if (seenRunning || Date.now() - started > 30_000) {
          return;
        }
      } catch {
        return;
      }
      if (Date.now() - started > 20 * 60 * 1000) return;
      await new Promise((resolve) => setTimeout(resolve, 2000));
    }
  }, []);

  const runAll = async (kind: "signin" | "refresh") => {
    const ok = await confirm({
      message:
        kind === "signin"
          ? t("confirmSigninAll", "Sign in on every enabled site now?")
          : t("confirmRefreshAll", "Refresh the statistics of every enabled site now?"),
    });
    if (!ok) return;
    setBulk(kind);
    try {
      const res = await apiSend<{ started: boolean; queued: number }>("POST", "/api/pt/actions/run-all", { kind });
      toast.success(t("bulkStarted", "Started the run for {{count}} sites.", { count: res.queued }));
      await waitForTick();
      await load();
    } catch (err) {
      const code = err instanceof ApiError ? err.status : 0;
      if (code === 409) {
        toast.warning(t("bulkBusy", "A run is already in progress."));
      } else if (code === 503) {
        toast.error(t("bulkUnavailable", "The scheduler is unavailable on this instance."));
      } else {
        toast.error(t("bulkFailed", "The run did not start. Try again."));
      }
    } finally {
      setBulk(null);
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
      <Button variant="outline" size="sm" onClick={() => void runAll("signin")} disabled={bulk !== null || trackers.length === 0}>
        <CalendarCheck className="h-3.5 w-3.5" />
        {bulk === "signin" ? t("bulkRunning", "Running…") : t("signinAll", "Sign in all")}
      </Button>
      <Button variant="outline" size="sm" onClick={() => void runAll("refresh")} disabled={bulk !== null || trackers.length === 0}>
        <RefreshCw className="h-3.5 w-3.5" />
        {bulk === "refresh" ? t("bulkRunning", "Running…") : t("refreshAll", "Refresh all")}
      </Button>
      <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
        <RefreshCw className="h-3.5 w-3.5" />
        {t("reload", "Reload")}
      </Button>
      <Button
        size="sm"
        onClick={() => {
          setEditing(undefined);
          setFormOpen(true);
        }}
      >
        <Plus className="h-3.5 w-3.5" />
        {t("addTracker", "Add site")}
      </Button>
    </div>
  );

  if (loading) {
    return (
      <PageShell
        title={t("pageTitle", "PT Accounts")}
        description={t("pageDescription", "Private-tracker accounts: logins, daily sign-in, and traffic statistics.")}
        action={headerAction}
      >
        <div
          role="status"
          aria-busy="true"
          aria-label={t("loading", "Loading accounts…")}
        >
          <div className="mb-6 grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-6">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="rounded-lg border border-border bg-card p-5">
                <Skeleton className="mb-3 h-4 w-20" />
                <Skeleton className="h-6 w-16" />
              </div>
            ))}
          </div>

          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {[0, 1, 2].map((i) => (
              <div key={i} className="flex h-full flex-col rounded-lg border border-border bg-card p-5">
                <div className="mb-3 flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <Skeleton className="h-5 w-32" />
                    <div className="mt-2 flex flex-wrap items-center gap-1.5">
                      <Skeleton className="h-5 w-16 rounded-md" />
                      <Skeleton className="h-5 w-20 rounded-md" />
                      <Skeleton className="h-5 w-20 rounded-md" />
                    </div>
                  </div>
                  <Skeleton className="h-8 w-8 shrink-0 rounded-[var(--radius)]" />
                </div>

                <div className="mb-3 grid grid-cols-2 gap-3 sm:grid-cols-3">
                  {Array.from({ length: 6 }).map((_, m) => (
                    <div key={m} className="space-y-1">
                      <Skeleton className="h-3 w-12" />
                      <Skeleton className="h-4 w-16" />
                    </div>
                  ))}
                </div>

                <div className="mb-4 space-y-1.5">
                  <Skeleton className="h-3.5 w-44" />
                  <Skeleton className="h-3.5 w-40" />
                </div>

                <div className="mt-auto flex flex-wrap items-center gap-2">
                  <Skeleton className="h-8 w-24 rounded-[var(--radius)]" />
                  <Skeleton className="h-8 w-24 rounded-[var(--radius)]" />
                  <Skeleton className="h-8 w-16 rounded-[var(--radius)]" />
                  <Skeleton className="ml-auto h-8 w-8 rounded-[var(--radius)]" />
                </div>
              </div>
            ))}
          </div>
        </div>
      </PageShell>
    );
  }

  if (status === 402 || status === 404) {
    return (
      <PageShell
        title={t("pageTitle", "PT Accounts")}
        description={t("pageDescription", "Private-tracker accounts: logins, daily sign-in, and traffic statistics.")}
      >
        <LockedFallback
          status={status}
          feature={t("pageTitle", "PT Accounts")}
          description={t("pageDescription", "Private-tracker accounts: logins, daily sign-in, and traffic statistics.")}
        />
      </PageShell>
    );
  }

  if (status !== null) {
    const forbidden = status === 403;
    return (
      <PageShell
        title={t("pageTitle", "PT Accounts")}
        description={t("pageDescription", "Private-tracker accounts: logins, daily sign-in, and traffic statistics.")}
        action={headerAction}
      >
        <LoadFailure
          status={status}
          feature={t("pageTitle", "PT Accounts")}
          description={t("pageDescription", "Private-tracker accounts: logins, daily sign-in, and traffic statistics.")}
          errorText={forbidden ? t("errorForbidden", "You do not have permission.") : t("loadFailedReason", "The accounts could not be loaded.")}
          deniedTitle={t("errorForbidden", "You do not have permission.")}
          deniedBody={t("loadFailedReason", "The accounts could not be loaded.")}
          onRetry={forbidden ? undefined : () => void load()}
        />
      </PageShell>
    );
  }

  return (
    <PageShell
      title={t("pageTitle", "PT Accounts")}
      description={t("pageDescription", "Private-tracker accounts: logins, daily sign-in, and traffic statistics.")}
      action={headerAction}
    >
      {overview && !overview.scheduler_available ? (
        <Alert variant="warning" icon={<AlertTriangle className="h-4 w-4" />} className="mb-4">
          {t("schedulerOff")}
        </Alert>
      ) : null}

      <div className="mb-6 grid grid-cols-2 gap-3 lg:grid-cols-3 xl:grid-cols-6">
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
        <>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t("filterNamePlaceholder", "Filter by name or domain")}
              aria-label={t("filterNamePlaceholder", "Filter by name or domain")}
              className="min-w-0 flex-1 sm:max-w-xs"
            />
            <Select
              value={statusFilter}
              onValueChange={setStatusFilter}
              options={statusOptions}
              aria-label={t("filterStatus", "Status")}
              className="w-44"
            />
            <span className="text-[11px] text-muted-foreground">
              {t("filterCount", "{{shown}} of {{total}} sites", {
                shown: visible.length,
                total: trackers.length,
              })}
            </span>
            {filtering ? (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setQuery("");
                  setStatusFilter("all");
                }}
              >
                {t("clearFilters", "Clear filters")}
              </Button>
            ) : null}
          </div>

          {visible.length === 0 ? (
            <PageEmpty
              title={t("filterEmptyTitle", "No site matches the filter")}
              detail={t("filterEmptyDesc", "Change the name or status filter.")}
              action={
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setQuery("");
                    setStatusFilter("all");
                  }}
                >
                  {t("clearFilters", "Clear filters")}
                </Button>
              }
            />
          ) : (
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
              {visible.map((tracker) => (
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
        </>
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
