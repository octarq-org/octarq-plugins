import { useEffect, useState } from "react";
import {
  AreaChart,
  Badge,
  Modal,
  Skeleton,
  TabBar,
  Table,
  TBody,
  TD,
  TH,
  THead,
  TR,
  TableSkeleton,
} from "@octarq/plugin-sdk";
import type { TFunc } from "@octarq/plugin-sdk";
import { apiGet } from "../api";
import type { SigninResult, Snapshot, Tracker } from "../types";
import { formatBytes, formatDateTime, formatRatio, outcomeKey, outcomeTone } from "../format";

type Tab = "overview" | "traffic" | "signins";

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
      <TabBar
        value={tab}
        onValueChange={(value) => setTab(value as Tab)}
        size="sm"
        className="mb-4"
        items={[
          { value: "overview", label: t("tabOverview", "Overview") },
          { value: "traffic", label: t("tabTraffic", "Traffic") },
          { value: "signins", label: t("tabSignins", "Sign-ins") },
        ]}
      />

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
          <div
            role="status"
            aria-busy="true"
            aria-label={t("loading", "Loading accounts…")}
            className="space-y-4"
          >
            {[0, 1].map((i) => (
              <div key={i}>
                <Skeleton className="mb-1 h-4 w-20" />
                <Skeleton className="h-[120px] w-full rounded-lg" />
              </div>
            ))}
          </div>
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
          <TableSkeleton
            columnsCount={3}
            rowsCount={4}
            ariaLabel={t("loading", "Loading accounts…")}
          />
        ) : signins.length === 0 ? (
          <p className="py-6 text-center text-[13px] text-muted-foreground">{t("historyEmpty", "No history yet.")}</p>
        ) : (
          <div className="max-h-[50vh] overflow-y-auto rounded-lg border border-border">
            <Table>
              <THead>
                <TR>
                  <TH>{t("tableTime", "Time")}</TH>
                  <TH>{t("tableOutcome", "Result")}</TH>
                  <TH>{t("tableMessage", "Message")}</TH>
                </TR>
              </THead>
              <TBody>
                {signins.map((row) => (
                  <TR key={row.id} className="align-top">
                    <TD className="whitespace-nowrap text-xs text-muted-foreground">
                      {formatDateTime(row.attempted_at, unknown, lang)}
                    </TD>
                    <TD>
                      <Badge tone={outcomeTone(row.outcome)} shape="square">
                        {outcomeLabel(t, row.outcome)}
                      </Badge>
                    </TD>
                    <TD className="text-xs text-foreground">{row.message || row.reason || unknown}</TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          </div>
        )
      ) : null}
    </Modal>
  );
}
