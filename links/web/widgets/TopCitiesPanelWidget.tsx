import { Panel, BarList, useTranslation, useOverviewData, type StatKV } from "@octarq/plugin-sdk";

export default function TopCitiesPanelWidget() {
  const o = useOverviewData();
  const { t } = useTranslation();

  if (!o || o.cities === undefined) return null;

  const cities = (o.cities as StatKV[]) ?? [];
  const hasClicksInWindow = (o.clicks30d ?? 0) > 0;

  return (
    <Panel title={`${t("links.topCities")}`}>
      <BarList rows={cities} empty={
        hasClicksInWindow ? (
          <span>
            {t("links.noGeoData")}{" "}
            <a href="/admin/help" className="text-accent-fg hover:underline">
              {t("links.noGeoDataHelp")}
            </a>
          </span>
        ) : (
          t("links.noClickData")
        )
      } />
    </Panel>
  );
}
