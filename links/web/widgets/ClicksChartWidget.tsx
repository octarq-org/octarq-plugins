import { Panel, AreaChart, useTranslation, useOverviewData, type StatKV } from "@octarq/plugin-sdk";

export default function ClicksChartWidget() {
  const o = useOverviewData();
  const { t } = useTranslation();

  if (!o || o.series === undefined) return null;

  const series = (o.series as StatKV[]) ?? [];
  const clicks30d = (o.clicks30d as number) ?? 0;
  const botClicks30d = (o.botClicks30d as number) ?? 0;

  const botLabel = t("links.botHiddenLabel", { count: botClicks30d });

  return (
    <Panel
      className="mb-6"
      title={t("links.clicksLast30")}
      action={
        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground">
            {t("links.clicksTotal", { count: clicks30d })} · {botLabel}
          </span>
        </div>
      }
    >
      <AreaChart series={series} />
    </Panel>
  );
}
