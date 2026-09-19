import { useState, useEffect } from "react";
import { Modal, Button, Field, Input, Alert, FormError, Skeleton } from "@octarq/plugin-sdk";
import { AlertTriangle, Copy, Check } from "lucide-react";
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
          <div className="space-y-3">
            <Skeleton className="h-48 rounded-lg" />
            <Skeleton className="h-9 rounded-lg" />
            <Skeleton className="h-9 rounded-lg" />
          </div>
        ) : error ? (
          <FormError err={error} />
        ) : (
          <>
            {/* Scannable QR code */}
            {qrDataUrl && (
              <div className="flex flex-col items-center justify-center rounded-lg border border-border bg-well p-4">
                <img
                  src={qrDataUrl}
                  alt="TOTP QR Code"
                  className="h-48 w-48 rounded bg-white p-2 shadow-md"
                />
                <p className="mt-2 text-xs text-muted-foreground">
                  {t("twofa.scanHelp", "Scan with Google Authenticator, 1Password, or any authenticator app.")}
                </p>
              </div>
            )}

            {/* Secret key */}
            {detail?.secret && (
              <Field label={t("twofa.secretSeed", "Secret Seed (Base32)")}>
                <div className="flex items-center gap-2">
                  <Input readOnly value={detail.secret} className="select-all font-mono text-sm" />
                  <Button variant="outline" onClick={copySecret}>
                    {copiedSecret ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                    {copiedSecret ? t("twofa.copied", "Copied!") : t("twofa.copy", "Copy")}
                  </Button>
                </div>
              </Field>
            )}

            {/* otpauth URI */}
            {uri && (
              <Field label={t("twofa.keyURI", "Standard Key URI")}>
                <div className="flex items-center gap-2">
                  <Input readOnly value={uri} className="select-all font-mono text-xs" />
                  <Button variant="outline" onClick={copyURI}>
                    {copiedURI ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                    {copiedURI ? t("twofa.copied", "Copied!") : t("twofa.copy", "Copy")}
                  </Button>
                </div>
              </Field>
            )}

            {/* Security audit notice */}
            <Alert variant="warning" icon={<AlertTriangle className="h-4 w-4" />}>
              {t("twofa.auditNotice", "Revealing this secret has been recorded in the workspace security audit trail.")}
            </Alert>
          </>
        )}

        <div className="flex justify-end border-t border-border pt-2">
          <Button onClick={onClose}>{t("twofa.close", "Close")}</Button>
        </div>
      </div>
    </Modal>
  );
}
