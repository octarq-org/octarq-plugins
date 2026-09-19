import { useState } from "react";
import {
  Modal,
  Button,
  Field,
  Input,
  Textarea,
  Tabs,
  Alert,
  useToast,
} from "@octarq/plugin-sdk";
import { AlertTriangle, Copy } from "lucide-react";

interface Props {
  onClose: () => void;
  onImported: () => void;
  t: (key: string, fallback?: string) => string;
}

export function ImportExportModal({ onClose, onImported, t }: Props) {
  const toast = useToast();
  const [tab, setTab] = useState<"import" | "export">("import");
  const [importText, setImportText] = useState("");
  const [tags, setTags] = useState("");
  const [loading, setLoading] = useState(false);
  const [exportData, setExportData] = useState<string>("");
  const [copied, setCopied] = useState(false);

  const handleImport = async () => {
    const lines = importText
      .split("\n")
      .map((l) => l.trim())
      .filter((l) => l.startsWith("otpauth://"));

    if (lines.length === 0) {
      toast.error(t("twofa.errNoValidURIs", "No valid otpauth:// lines found."));
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
        toast.success(
          `${t("twofa.importedSuccess", "Successfully imported")} ${data.imported} ${t("twofa.accounts", "accounts.")}`
        );
        onImported();
      } else {
        toast.error(data.error || "Import failed");
      }
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Network error");
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
      toast.error(t("twofa.exportFailed", "Failed to fetch export data"));
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

  const importPanel = (
    <div className="space-y-3">
      <Field label={t("twofa.pasteURIs", "Paste otpauth:// URIs (one per line)")}>
        <Textarea
          rows={6}
          className="font-mono text-xs"
          placeholder={"otpauth://totp/GitHub:user?secret=JBSWY3DPEHPK3PXP\notpauth://totp/AWS:root?secret=GEZDGNBVGY3TQOJQ"}
          value={importText}
          onChange={(e) => setImportText(e.target.value)}
        />
      </Field>
      <Field label={t("twofa.importTags", "Tags to apply to all imported items")}>
        <Input placeholder="Imported, 2026-Batch" value={tags} onChange={(e) => setTags(e.target.value)} />
      </Field>

      <div className="flex justify-end gap-2 pt-2">
        <Button onClick={handleImport} disabled={loading || !importText.trim()}>
          {loading ? t("twofa.importing", "Importing...") : t("twofa.startImport", "Start Import")}
        </Button>
      </div>
    </div>
  );

  const exportPanel = (
    <div className="space-y-3">
      <Alert variant="warning" icon={<AlertTriangle className="h-4 w-4" />}>
        {t("twofa.exportWarning", "Exported JSON contains plaintext secret URIs. Store this backup securely and never share it publicly.")}
      </Alert>

      {loading ? (
        <div className="py-8 text-center text-muted-foreground">{t("twofa.exporting", "Exporting accounts...")}</div>
      ) : (
        <>
          <Textarea
            readOnly
            rows={8}
            className="select-all font-mono text-xs"
            value={exportData}
          />
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={handleCopyExport}>
              <Copy className="h-3.5 w-3.5" />
              {copied ? t("twofa.copied", "Copied!") : t("twofa.copyJSON", "Copy JSON")}
            </Button>
          </div>
        </>
      )}
    </div>
  );

  return (
    <Modal
      wide
      onClose={onClose}
      title={t("twofa.importExportTitle", "Import / Export Accounts")}
    >
      <div className="space-y-4 text-sm">
        <Tabs
          value={tab}
          onValueChange={(v) => {
            const next = v as "import" | "export";
            setTab(next);
            if (next === "export" && !exportData) handleExport();
          }}
          items={[
            { value: "import", label: t("twofa.importTab", "Batch Import"), content: importPanel },
            { value: "export", label: t("twofa.exportTab", "Export Backup"), content: exportPanel },
          ]}
        />

        <div className="flex justify-end border-t border-border pt-2">
          <Button onClick={onClose}>{t("twofa.close", "Close")}</Button>
        </div>
      </div>
    </Modal>
  );
}
