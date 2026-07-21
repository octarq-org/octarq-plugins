// Settings page for the Telegram connector. Reads/writes the backend
// (/api/telegram/settings) and sends a test message (/api/telegram/test),
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
  chatId: string;
  hasToken: boolean;
}

export default function TelegramPage() {
  const { t } = useTranslation();
  const [botToken, setBotToken] = useState("");
  const [chatId, setChatId] = useState("");
  const [hasToken, setHasToken] = useState(false);
  const [status, setStatus] = useState<number | null>(null);
  const [msg, setMsg] = useState("");

  useEffect(() => {
    fetch("/api/telegram/settings", { credentials: "same-origin" })
      .then((res) => {
        if (!res.ok) {
          setStatus(res.status);
          return null;
        }
        return res.json() as Promise<Settings>;
      })
      .then((s) => {
        if (s) {
          setChatId(s.chatId);
          setHasToken(s.hasToken);
        }
      })
      .catch(() => setStatus(0));
  }, []);

  if (status === 402 || status === 404) {
    return (
      <ScreenWrap>
        <LockedFallback status={status} feature="Telegram" description={t("telegram.pageDesc")} />
      </ScreenWrap>
    );
  }

  async function save() {
    const res = await fetch("/api/telegram/settings", {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ botToken, chatId }),
    });
    if (res.ok) {
      const s = (await res.json()) as Settings;
      setHasToken(s.hasToken);
      setBotToken("");
      setMsg(t("telegram.saved", "Saved."));
    }
  }

  async function test() {
    const res = await fetch("/api/telegram/test", { method: "POST", credentials: "same-origin" });
    setMsg(res.ok ? t("telegram.testSent", "Test message sent.") : `Error ${res.status}`);
  }

  return (
    <ScreenWrap>
      <PageHeader title={t("telegram.pageTitle", "Telegram")} description={t("telegram.pageDesc")} />
      <GlassCard className="max-w-lg space-y-4 p-6">
        <Field label={t("telegram.botToken", "Bot token")}>
          <input
            type="password"
            className="w-full rounded bg-white/5 px-3 py-2 text-sm"
            placeholder={hasToken ? "•••••••• (saved)" : "123456:ABC-..."}
            value={botToken}
            onChange={(e) => setBotToken(e.target.value)}
          />
        </Field>
        <Field label={t("telegram.chatId", "Chat ID")}>
          <input
            className="w-full rounded bg-white/5 px-3 py-2 text-sm"
            placeholder="123456789"
            value={chatId}
            onChange={(e) => setChatId(e.target.value)}
          />
        </Field>
        <div className="flex items-center gap-3">
          <Button onClick={save}>{t("telegram.save", "Save")}</Button>
          <Button variant="ghost" onClick={test} disabled={!hasToken}>
            {t("telegram.test", "Send test")}
          </Button>
          {msg && <span className="text-sm text-white/50">{msg}</span>}
        </div>
      </GlassCard>
    </ScreenWrap>
  );
}
