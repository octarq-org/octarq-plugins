import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { SetupStep, useTranslation } from "@octarq/plugin-sdk";
import { api, Overview } from "../api";

export default function LinksSetupStepWidget() {
  const [o, setO] = useState<Overview | null>(null);
  const nav = useNavigate();
  const { t } = useTranslation();

  useEffect(() => {
    api.overview().then(setO).catch(() => {});
  }, []);

  if (!o || o.links === undefined) return null;

  const completed = (o.links ?? 0) > 0;

  return (
    <SetupStep
      title={t("overview.stepLinkTitle")}
      description={t("overview.stepLinkDesc")}
      completed={completed}
      onClick={() => nav("/links")}
    />
  );
}
