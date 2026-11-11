import { useNavigate } from "react-router-dom";
import { SetupStep, useOverviewData, useTranslation } from "@octarq/plugin-sdk";

export default function DNSSetupStepWidget() {
  const o = useOverviewData();
  const nav = useNavigate();
  const { t } = useTranslation();

  if (!o || o.domains === undefined) return null;

  const completed = (o.domains ?? 0) > 0;

  return (
    <SetupStep
      title={t("overview.stepDomainTitle")}
      description={t("overview.stepDomainDesc")}
      completed={completed}
      onClick={() => nav("/domains")}
    />
  );
}
