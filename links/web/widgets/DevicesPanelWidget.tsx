import { Panel, BarList, useTranslation, useOverviewData, type StatKV } from "@octarq/plugin-sdk";

export default function DevicesPanelWidget() {
  const o = useOverviewData();
  const { t } = useTranslation();

  if (!o || o.devices === undefined) return null;

  const devices = (o.devices as StatKV[]) ?? [];

  return (
    <Panel title={`${t("links.devices")}`}>
      <BarList rows={devices} />
    </Panel>
  );
}
