// Settings page for the Webhook connector. Reads/writes the backend
// (/api/webhook/settings) and sends a test message (/api/webhook/test),
// handling the standard 402/404 gated states via <LockedFallback />.
import { useEffect, useState } from "react";
import {
  ScreenWrap,
  PageHeader,
  GlassCard,
  Button,
  Field,
  LockedFallback,
  useTranslation,
} from "@octarq/plugin-sdk";

interface Settings {
  url: string;
}

export default function WebhookPage() {
  const { t } = useTranslation();
  const [url, setUrl] = useState("");
  const [status, setStatus] = useState<number | null>(null);
  const [msg, setMsg] = useState("");

  useEffect(() => {
    fetch("/api/webhook/settings", { credentials: "same-origin" })
      .then((res) => {
        if (!res.ok) {
          setStatus(res.status);
          return null;
        }
        return res.json() as Promise<Settings>;
      })
      .then((s) => {
        if (s) {
          setUrl(s.url || "");
        }
      })
      .catch(() => setStatus(0));
  }, []);

  if (status === 402 || status === 404) {
    return (
      <ScreenWrap>
        <LockedFallback status={status} feature="Webhook" description={t("webhook.pageDesc")} />
      </ScreenWrap>
    );
  }

  async function save() {
    const res = await fetch("/api/webhook/settings", {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url }),
    });
    if (res.ok) {
      const s = (await res.json()) as Settings;
      setUrl(s.url || "");
      setMsg(t("webhook.saved", "Saved."));
    }
  }

  async function test() {
    const res = await fetch("/api/webhook/test", { method: "POST", credentials: "same-origin" });
    setMsg(res.ok ? t("webhook.testSent", "Test message sent.") : `Error ${res.status}`);
  }

  return (
    <ScreenWrap>
      <PageHeader title={t("webhook.pageTitle", "Webhook")} description={t("webhook.pageDesc")} />
      <GlassCard className="max-w-lg space-y-4 p-6">
        <Field label={t("webhook.url", "Webhook URL")}>
          <input
            className="w-full rounded bg-white/5 px-3 py-2 text-sm"
            placeholder="https://example.com/webhook"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
          />
        </Field>
        <div className="flex items-center gap-3">
          <Button onClick={save}>{t("webhook.save", "Save")}</Button>
          <Button variant="ghost" onClick={test} disabled={!url}>
            {t("webhook.test", "Send test")}
          </Button>
          {msg && <span className="text-sm text-white/50">{msg}</span>}
        </div>
      </GlassCard>
    </ScreenWrap>
  );
}
