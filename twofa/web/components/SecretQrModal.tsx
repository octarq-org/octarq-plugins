import { useState, useEffect } from "react";
import { Modal, Button, Field } from "@octarq/plugin-sdk";
import type { AccountSummary, AccountDetail } from "../types";

interface Props {
  onClose: () => void;
  account: AccountSummary | null;
  t: (key: string, fallback?: string) => string;
}

export function SecretQrModal({ onClose, account, t }: Props) {
  const [detail, setDetail] = useState<AccountDetail | null>(null);
  const [qrDataUrl, setQrDataUrl] = useState("");
  const [uri, setUri] = useState("");
  const [loading, setLoading] = useState(false);
  const [copiedSecret, setCopiedSecret] = useState(false);
  const [copiedURI, setCopiedURI] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!account) {
      setDetail(null);
      setQrDataUrl("");
      setUri("");
      setError("");
      return;
    }

    setLoading(true);
    // Fetch secret with reveal=true
    Promise.all([
      fetch(`/api/twofa/accounts/${account.id}?reveal=true`, {
        credentials: "same-origin",
      }).then((res) => (res.ok ? (res.json() as Promise<AccountDetail>) : null)),
      fetch(`/api/twofa/accounts/${account.id}/qr`, {
        credentials: "same-origin",
      }).then((res) => (res.ok ? res.json() : null)),
    ])
      .then(([accData, qrData]) => {
        if (accData) setDetail(accData);
        if (qrData) {
          setQrDataUrl(qrData.qr_data_url || "");
          setUri(qrData.uri || "");
        }
        setLoading(false);
      })
      .catch((err) => {
        setError(err.message || "Failed to load secret and QR code");
        setLoading(false);
      });
  }, [account]);

  const copySecret = () => {
    if (!detail?.secret) return;
    navigator.clipboard.writeText(detail.secret);
    setCopiedSecret(true);
    setTimeout(() => setCopiedSecret(false), 1800);
  };

  const copyURI = () => {
    if (!uri) return;
    navigator.clipboard.writeText(uri);
    setCopiedURI(true);
    setTimeout(() => setCopiedURI(false), 1800);
  };

  if (!account) return null;

  return (
    <Modal
      onClose={onClose}
      title={`${account.name} — ${t("twofa.qrModalTitle", "QR Code & Secret")}`}
    >
      <div className="space-y-4 text-sm">
        {loading ? (
          <div className="py-8 text-center text-white/50">
            {t("twofa.loading", "Loading secure credentials...")}
          </div>
        ) : error ? (
          <div className="text-red-400">{error}</div>
        ) : (
          <>
            {/* Scannable QR Code */}
            {qrDataUrl && (
              <div className="flex flex-col items-center justify-center p-4 bg-white/5 rounded-lg border border-white/10">
                <img
                  src={qrDataUrl}
                  alt="TOTP QR Code"
                  className="h-48 w-48 rounded bg-white p-2 shadow-md"
                />
                <p className="mt-2 text-xs text-white/50">
                  {t("twofa.scanHelp", "Scan with Google Authenticator, 1Password, or any authenticator app.")}
                </p>
              </div>
            )}

            {/* Secret key */}
            {detail?.secret && (
              <Field label={t("twofa.secretSeed", "Secret Seed (Base32)")}>
                <div className="flex items-center gap-2">
                  <input
                    readOnly
                    type="text"
                    value={detail.secret}
                    className="w-full rounded bg-white/5 px-3 py-2 font-mono text-sm text-emerald-400 border border-white/10 select-all"
                  />
                  <Button variant="ghost" onClick={copySecret}>
                    {copiedSecret ? t("twofa.copied", "Copied!") : t("twofa.copy", "Copy")}
                  </Button>
                </div>
              </Field>
            )}

            {/* otpauth URI */}
            {uri && (
              <Field label={t("twofa.keyURI", "Standard Key URI")}>
                <div className="flex items-center gap-2">
                  <input
                    readOnly
                    type="text"
                    value={uri}
                    className="w-full rounded bg-white/5 px-3 py-2 font-mono text-xs text-white/70 border border-white/10 select-all"
                  />
                  <Button variant="ghost" onClick={copyURI}>
                    {copiedURI ? t("twofa.copied", "Copied!") : t("twofa.copy", "Copy")}
                  </Button>
                </div>
              </Field>
            )}

            {/* Security Audit Notice */}
            <div className="rounded-lg bg-amber-500/10 p-3 border border-amber-500/20 text-xs text-amber-300">
              ⚠️ {t("twofa.auditNotice", "Revealing this secret has been recorded in the workspace security audit trail.")}
            </div>
          </>
        )}

        <div className="flex justify-end pt-2 border-t border-white/10">
          <Button onClick={onClose}>{t("twofa.close", "Close")}</Button>
        </div>
      </div>
    </Modal>
  );
}
