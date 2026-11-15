import { useEffect, useState } from "react";
import { AreaChart, Badge, Modal, Skeleton } from "@octarq/plugin-sdk";
import type { TFunc } from "@octarq/plugin-sdk";
import { apiGet } from "../api";
import type { SigninResult, Snapshot, Tracker } from "../types";
import { formatBytes, formatDateTime, formatRatio, outcomeKey, outcomeTone } from "../format";

type Tab = "overview" | "traffic" | "signins";

// TrackerDetail shows the stored history of one account. Both lists are read
// only: the page never changes data from here.
export interface TrackerDetailProps {
  tracker: Tracker;
  onClose: () => void;
  t: TFunc;
  lang: string;
}

function outcomeLabel(t: TFunc, outcome: string): string {
  return t(outcomeKey(outcome), outcome);
}

export function TrackerDetail({ tracker, onClose, t, lang }: TrackerDetailProps) {
  const [tab, setTab] = useState<Tab>("overview");
  const [snapshots, setSnapshots] = useState<Snapshot[] | null>(null);
  const [signins, setSignins] = useState<SigninResult[] | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let alive = true;
    Promise.all([
      apiGet<Snapshot[]>(`/api/pt/trackers/${tracker.id}/snapshots?limit=90`),
      apiGet<SigninResult[]>(`/api/pt/trackers/${tracker.id}/signins?limit=30`),
    ])
      .then(([snaps, rows]) => {
        if (!alive) return;
        setSnapshots(snaps ?? []);
        setSignins(rows ?? []);
      })
      .catch(() => {
        if (alive) setFailed(true);
      });
    return () => {
      alive = false;
    };
  }, [tracker.id]);

  const latest = tracker.latest_snapshot;
  const unknown = t("unknown", "—");

  return (
    <Modal title={tracker.name} onClose={onClose} wide>
      <div className="mb-4 flex gap-1 rounded-lg border border-border p-1">
        {(["overview", "traffic", "signins"] as Tab[]).map((key) => (
          <button
            key={key}
            type="button"
            onClick={() => setTab(key)}
            className={
              tab === key
                ? "flex-1 rounded-md bg-muted px-3 py-1.5 text-[12.5px] font-semibold text-foreground"
                : "flex-1 rounded-md px-3 py-1.5 text-[12.5px] text-muted-foreground hover:text-foreground"
            }
          >
            {key === "overview"
              ? t("tabOverview", "Overview")
              : key === "traffic"
                ? t("tabTraffic", "Traffic")
                : t("tabSignins", "Sign-ins")}
          </button>
        ))}
      </div>

      {failed ? (
        <p className="py-6 text-center text-[13px] text-muted-foreground">{t("errorGeneric", "The action failed.")}</p>
      ) : null}

      {!failed && tab === "overview" ? (
        <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-[13px]">
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t("level", "Level")}</dt>
            <dd className="text-foreground">{latest?.user_level || unknown}</dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t("ratio", "Ratio")}</dt>
            <dd className="text-foreground">{formatRatio(latest?.ratio, latest?.ratio_infinite ?? false, unknown)}</dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t("uploaded", "Upload")}</dt>
            <dd className="text-foreground">{formatBytes(latest?.uploaded_bytes, unknown)}</dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t("downloaded", "Download")}</dt>
            <dd className="text-foreground">{formatBytes(latest?.downloaded_bytes, unknown)}</dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t("bonus", "Bonus")}</dt>
            <dd className="text-foreground">{latest?.bonus === undefined ? unknown : Number(latest.bonus).toLocaleString()}</dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t("warnings", "Warnings")}</dt>
            <dd className="text-foreground">{latest?.warning_count === undefined ? unknown : latest.warning_count}</dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t("nextSignin", "Next sign-in")}</dt>
            <dd className="text-foreground">{formatDateTime(tracker.next_signin_at, t("never", "Never"), lang)}</dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wide text-muted-foreground">{t("nextStats", "Next refresh")}</dt>
            <dd className="text-foreground">{formatDateTime(tracker.next_stats_at, t("never", "Never"), lang)}</dd>
          </div>
        </dl>
      ) : null}

      {!failed && tab === "traffic" ? (
        snapshots === null ? (
          <Skeleton className="h-32 w-full" />
        ) : snapshots.length === 0 ? (
          <p className="py-6 text-center text-[13px] text-muted-foreground">{t("historyEmpty", "No history yet.")}</p>
        ) : (
          <div className="space-y-4">
            <div>
              <div className="mb-1 text-[12px] text-muted-foreground">{t("uploaded", "Upload")}</div>
              <AreaChart
                series={[...snapshots].reverse().map((s) => ({
                  key: s.captured_at,
                  count: Math.round(((s.uploaded_bytes ?? 0) / 1024 ** 3) * 100) / 100,
                }))}
              />
            </div>
            <div>
              <div className="mb-1 text-[12px] text-muted-foreground">{t("downloaded", "Download")}</div>
              <AreaChart
                series={[...snapshots].reverse().map((s) => ({
                  key: s.captured_at,
                  count: Math.round(((s.downloaded_bytes ?? 0) / 1024 ** 3) * 100) / 100,
                }))}
              />
            </div>
          </div>
        )
      ) : null}

      {!failed && tab === "signins" ? (
        signins === null ? (
          <Skeleton className="h-32 w-full" />
        ) : signins.length === 0 ? (
          <p className="py-6 text-center text-[13px] text-muted-foreground">{t("historyEmpty", "No history yet.")}</p>
        ) : (
          <div className="max-h-[50vh] overflow-y-auto">
            <table className="w-full text-left text-[12.5px]">
              <thead className="text-[11px] uppercase tracking-wide text-muted-foreground">
                <tr>
                  <th className="pb-2 pr-3 font-medium">{t("tableTime", "Time")}</th>
                  <th className="pb-2 pr-3 font-medium">{t("tableOutcome", "Result")}</th>
                  <th className="pb-2 font-medium">{t("tableMessage", "Message")}</th>
                </tr>
              </thead>
              <tbody>
                {signins.map((row) => (
                  <tr key={row.id} className="border-t border-border align-top">
                    <td className="py-2 pr-3 whitespace-nowrap text-muted-foreground">
                      {formatDateTime(row.attempted_at, unknown, lang)}
                    </td>
                    <td className="py-2 pr-3">
                      <Badge tone={outcomeTone(row.outcome)} shape="square">
                        {outcomeLabel(t, row.outcome)}
                      </Badge>
                    </td>
                    <td className="py-2 text-foreground">{row.message || row.reason || unknown}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      ) : null}
    </Modal>
  );
}
