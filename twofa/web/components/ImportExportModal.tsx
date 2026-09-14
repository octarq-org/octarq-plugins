import { useState } from "react";
import { Modal, Button, Field, Input } from "@octarq/plugin-sdk";

interface Props {
  onClose: () => void;
  onImported: () => void;
  t: (key: string, fallback?: string) => string;
}

export function ImportExportModal({ onClose, onImported, t }: Props) {
  const [tab, setTab] = useState<"import" | "export">("import");
  const [importText, setImportText] = useState("");
  const [tags, setTags] = useState("");
  const [loading, setLoading] = useState(false);
  const [resultMsg, setResultMsg] = useState("");
  const [exportData, setExportData] = useState<string>("");
  const [copied, setCopied] = useState(false);

  const handleImport = async () => {
    setResultMsg("");
    const lines = importText
      .split("\n")
      .map((l) => l.trim())
      .filter((l) => l.startsWith("otpauth://"));

    if (lines.length === 0) {
      setResultMsg(t("twofa.errNoValidURIs", "No valid otpauth:// lines found."));
      return;
    }

    setLoading(true);
    try {
      const res = await fetch("/api/twofa/import", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ uris: lines, tags: tags.trim() }),
      });
      const data = await res.json();
      if (res.ok) {
        setResultMsg(
          `${t("twofa.importedSuccess", "Successfully imported")} ${data.imported} ${t("twofa.accounts", "accounts.")}`
        );
        onImported();
      } else {
        setResultMsg(data.error || "Import failed");
      }
    } catch (err: unknown) {
      setResultMsg(err instanceof Error ? err.message : "Network error");
    } finally {
      setLoading(false);
    }
  };

  const handleExport = async () => {
    setLoading(true);
    try {
      const res = await fetch("/api/twofa/export", { credentials: "same-origin" });
      if (res.ok) {
        const data = await res.json();
        setExportData(JSON.stringify(data, null, 2));
      }
    } catch {
      setExportData("Failed to fetch export data");
    } finally {
      setLoading(false);
    }
  };

  const handleCopyExport = () => {
    if (!exportData) return;
    navigator.clipboard.writeText(exportData);
    setCopied(true);
    setTimeout(() => setCopied(false), 1800);
  };

  return (
    <Modal
      wide
      onClose={onClose}
      title={t("twofa.importExportTitle", "Import / Export Accounts")}
    >
      <div className="space-y-4 text-sm">
        <div className="flex border-b border-white/10 pb-2 gap-3 text-sm">
          <button
            onClick={() => setTab("import")}
            className={`pb-1 ${tab === "import" ? "border-b-2 border-emerald-400 font-semibold text-white" : "text-white/50 hover:text-white"}`}
          >
            {t("twofa.importTab", "Batch Import")}
          </button>
          <button
            onClick={() => {
              setTab("export");
              if (!exportData) handleExport();
            }}
            className={`pb-1 ${tab === "export" ? "border-b-2 border-emerald-400 font-semibold text-white" : "text-white/50 hover:text-white"}`}
          >
            {t("twofa.exportTab", "Export Backup")}
          </button>
        </div>

        {tab === "import" && (
          <div className="space-y-3">
            <Field label={t("twofa.pasteURIs", "Paste otpauth:// URIs (one per line)")}>
              <textarea
                className="w-full rounded bg-white/5 p-2 font-mono text-xs text-white border border-white/10 focus:border-emerald-500 focus:outline-none"
                rows={6}
                placeholder={"otpauth://totp/GitHub:user?secret=JBSWY3DPEHPK3PXP\notpauth://totp/AWS:root?secret=GEZDGNBVGY3TQOJQ"}
                value={importText}
                onChange={(e) => setImportText(e.target.value)}
              />
            </Field>
            <Field label={t("twofa.importTags", "Tags to apply to all imported items")}>
              <Input
                placeholder="Imported, 2026-Batch"
                value={tags}
                onChange={(e) => setTags(e.target.value)}
              />
            </Field>

            {resultMsg && (
              <div className="text-xs font-medium text-emerald-400">{resultMsg}</div>
            )}

            <div className="flex justify-end gap-2 pt-2">
              <Button onClick={handleImport} disabled={loading || !importText.trim()}>
                {loading ? t("twofa.importing", "Importing...") : t("twofa.startImport", "Start Import")}
              </Button>
            </div>
          </div>
        )}

        {tab === "export" && (
          <div className="space-y-3">
            <p className="text-xs text-amber-300 bg-amber-500/10 p-2.5 rounded border border-amber-500/20">
              ⚠️ {t("twofa.exportWarning", "Exported JSON contains plaintext secret URIs. Store this backup securely and never share it publicly.")}
            </p>

            {loading ? (
              <div className="py-8 text-center text-white/50">{t("twofa.exporting", "Exporting accounts...")}</div>
            ) : (
              <div className="relative">
                <textarea
                  readOnly
                  className="w-full rounded bg-white/5 p-2 font-mono text-xs text-white border border-white/10 select-all"
                  rows={8}
                  value={exportData}
                />
                <div className="flex justify-end gap-2 pt-2">
                  <Button variant="ghost" onClick={handleCopyExport}>
                    {copied ? t("twofa.copied", "Copied!") : t("twofa.copyJSON", "Copy JSON")}
                  </Button>
                </div>
              </div>
            )}
          </div>
        )}

        <div className="flex justify-end pt-2 border-t border-white/10">
          <Button onClick={onClose}>{t("twofa.close", "Close")}</Button>
        </div>
      </div>
    </Modal>
  );
}
