// The plugin's page — the JS half of the feature. It calls the Go half
// (GET /api/myplugin/ping) and renders with the shared UI from
// @octarq/plugin-sdk, so it matches the app and gets accessibility for free.
//
// A plugin can't import the host's internal API client, so it uses a plain
// fetch and handles the two gated states Octarq standardises on:
//   402 → the feature is unlicensed (show an upsell),
//   404 → the plugin isn't built into this installation (neutral note).
// Both are covered by the SDK's <LockedFallback status={…} />.
import { useEffect, useState } from "react";
import {
  PageShell,
  PageCard,
  Skeleton,
  LockedFallback,
  useTranslation,
} from "@octarq/plugin-sdk";

interface Ping {
  message: string;
  time: string;
}

export default function MyPluginPage() {
  const { t } = useTranslation();
  const [ping, setPing] = useState<Ping | null>(null);
  const [status, setStatus] = useState<number | null>(null);

  useEffect(() => {
    fetch("/api/myplugin/ping", { credentials: "same-origin" })
      .then((res) => {
        if (!res.ok) {
          setStatus(res.status);
          return null;
        }
        return res.json() as Promise<Ping>;
      })
      .then((data) => data && setPing(data))
      .catch(() => setStatus(0));
  }, []);

  // 402/404 → the standard gated fallback. Anything else falls through to a
  // neutral loading/empty state below (never a raw error).
  if (status === 402 || status === 404) {
    return (
      <PageShell title={t("myplugin.pageTitle", "My Plugin")}>
        <LockedFallback
          status={status}
          feature={t("myplugin.feature", "My Plugin")}
          description={t("myplugin.description", "A starter Octarq plugin.")}
        />
      </PageShell>
    );
  }

  return (
    <PageShell
      title={t("myplugin.pageTitle", "My Plugin")}
      description={t("myplugin.pageDesc", "A starter full-stack Octarq plugin.")}
    >
      <PageCard className="mt-6 p-6">
        {ping ? (
          <div className="space-y-1 text-sm">
            <p className="text-foreground">{ping.message}</p>
            <p className="text-muted-foreground">{ping.time}</p>
          </div>
        ) : (
          <div
            role="status"
            aria-busy="true"
            aria-label={t("myplugin.loading", "Loading…")}
            className="space-y-1"
          >
            <Skeleton className="h-5 w-48" />
            <Skeleton className="h-5 w-32" />
          </div>
        )}
      </PageCard>
    </PageShell>
  );
}
