import { useState, useEffect } from "react";
import {
  Modal,
  Button,
  Field,
  Input,
  Textarea,
  Select,
  Switch,
  Tabs,
  FormError,
} from "@octarq/plugin-sdk";
import type { AccountSummary, CreateAccountInput, UpdateAccountInput } from "../types";

interface Props {
  onClose: () => void;
  account?: AccountSummary | null;
  onSaved: () => void;
  t: (key: string, fallback?: string) => string;
}

export function AddEditModal({ onClose, account, onSaved, t }: Props) {
  const isEdit = !!account;
  const [tab, setTab] = useState<"manual" | "uri">("manual");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  // Form fields
  const [name, setName] = useState("");
  const [issuer, setIssuer] = useState("");
  const [accountName, setAccountName] = useState("");
  const [secret, setSecret] = useState("");
  const [algorithm, setAlgorithm] = useState("SHA1");
  const [digits, setDigits] = useState(6);
  const [period, setPeriod] = useState(30);
  const [tags, setTags] = useState("");
  const [notes, setNotes] = useState("");
  const [pinned, setPinned] = useState(false);
  const [uriInput, setUriInput] = useState("");

  useEffect(() => {
    if (account) {
      setName(account.name);
      setIssuer(account.issuer || "");
      setAccountName(account.account || "");
      setSecret(""); // secret remains untouched unless user enters a new one
      setAlgorithm(account.algorithm || "SHA1");
      setDigits(account.digits || 6);
      setPeriod(account.period || 30);
      setTags(account.tags || "");
      setNotes(account.notes || "");
      setPinned(account.pinned || false);
      setTab("manual");
    } else {
      setName("");
      setIssuer("");
      setAccountName("");
      setSecret("");
      setAlgorithm("SHA1");
      setDigits(6);
      setPeriod(30);
      setTags("");
      setNotes("");
      setPinned(false);
      setUriInput("");
      setTab("manual");
    }
    setError("");
  }, [account]);

  // Handle parsing of otpauth URI
  const handleParseURI = (raw: string) => {
    setUriInput(raw);
    setError("");
    if (!raw.trim().startsWith("otpauth://")) return;

    try {
      const parsed = new URL(raw.trim());
      if (parsed.protocol !== "otpauth:" || parsed.hostname !== "totp") {
        setError(t("twofa.errInvalidURI", "URI must start with otpauth://totp/"));
        return;
      }

      const q = parsed.searchParams;
      const s = q.get("secret");
      if (!s) {
        setError(t("twofa.errMissingSecret", "Secret is required in URI"));
        return;
      }

      setSecret(s.toUpperCase().replace(/\s+/g, ""));
      const iss = q.get("issuer") || "";
      setIssuer(iss);

      const label = decodeURIComponent(parsed.pathname.replace(/^\//, ""));
      if (label.includes(":")) {
        const [lIss, lAcc] = label.split(":");
        if (!iss) setIssuer(lIss.trim());
        setAccountName(lAcc.trim());
        setName(label);
      } else {
        setAccountName(label);
        setName(iss || label || "Unnamed 2FA");
      }

      const algo = q.get("algorithm");
      if (algo) setAlgorithm(algo.toUpperCase());
      const d = q.get("digits");
      if (d && (d === "6" || d === "8")) setDigits(parseInt(d, 10));
      const p = q.get("period");
      if (p) setPeriod(parseInt(p, 10));

      setTab("manual"); // Switch back to form so user sees parsed values
    } catch {
      setError(t("twofa.errMalformedURI", "Malformed otpauth URI"));
    }
  };

  const handleSave = async () => {
    setError("");
    if (!name.trim()) {
      setError(t("twofa.errNameRequired", "Account name is required"));
      return;
    }
    if (!isEdit && !secret.trim()) {
      setError(t("twofa.errSecretRequired", "Secret is required"));
      return;
    }

    setLoading(true);
    try {
      const url = isEdit
        ? `/api/twofa/accounts/${account.id}`
        : "/api/twofa/accounts";
      const method = isEdit ? "PUT" : "POST";

      const payload: CreateAccountInput | UpdateAccountInput = {
        name: name.trim(),
        issuer: issuer.trim(),
        account: accountName.trim(),
        algorithm,
        digits: Number(digits),
        period: Number(period),
        tags: tags.trim(),
        notes: notes.trim(),
        pinned,
      };

      if (secret.trim()) {
        payload.secret = secret.trim();
      }

      const res = await fetch(url, {
        method,
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });

      if (!res.ok) {
        const text = await res.text();
        setError(text || `Request failed with status ${res.status}`);
        setLoading(false);
        return;
      }

      setLoading(false);
      onSaved();
      onClose();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Network error");
      setLoading(false);
    }
  };

  const manualForm = (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3">
        <Field label={t("twofa.accountName", "Account Title")}>
          <Input placeholder="AWS Production" value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t("twofa.issuer", "Issuer / Service")}>
          <Input placeholder="Amazon Web Services" value={issuer} onChange={(e) => setIssuer(e.target.value)} />
        </Field>
      </div>

      <Field label={t("twofa.username", "Account / Username / Email")}>
        <Input placeholder="admin@octarq.com" value={accountName} onChange={(e) => setAccountName(e.target.value)} />
      </Field>

      <Field
        label={
          isEdit
            ? t("twofa.secretEdit", "Secret Key (Leave blank to keep unchanged)")
            : t("twofa.secret", "Base32 Secret Key")
        }
      >
        <Input
          type="password"
          placeholder={isEdit ? "••••••••••••" : "JBSWY3DPEHPK3PXP"}
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
        />
      </Field>

      <div className="grid grid-cols-3 gap-3">
        <Field label={t("twofa.algorithm", "Algorithm")}>
          <Select
            value={algorithm}
            onValueChange={setAlgorithm}
            options={[
              { value: "SHA1", label: "SHA1" },
              { value: "SHA256", label: "SHA256" },
              { value: "SHA512", label: "SHA512" },
            ]}
          />
        </Field>
        <Field label={t("twofa.digits", "Digits")}>
          <Select
            value={String(digits)}
            onValueChange={(v) => setDigits(parseInt(v, 10))}
            options={[
              { value: "6", label: "6 digits" },
              { value: "8", label: "8 digits" },
            ]}
          />
        </Field>
        <Field label={t("twofa.period", "Period (sec)")}>
          <Input
            type="number"
            value={period}
            onChange={(e) => setPeriod(parseInt(e.target.value, 10) || 30)}
          />
        </Field>
      </div>

      <Field label={t("twofa.tags", "Tags (comma-separated)")}>
        <Input
          placeholder="Cloud, Production, Critical"
          value={tags}
          onChange={(e) => setTags(e.target.value)}
        />
      </Field>

      <Field label={t("twofa.notes", "Notes")}>
        <Textarea
          rows={2}
          placeholder="Emergency recovery credentials stored in 1Password vault"
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
        />
      </Field>

      <div className="flex items-center gap-2 pt-1">
        <Switch checked={pinned} onCheckedChange={setPinned} aria-label={t("twofa.pinToTop", "Pin this account to top of dashboard")} />
        <span className="cursor-pointer text-xs text-muted-foreground" onClick={() => setPinned(!pinned)}>
          {t("twofa.pinToTop", "Pin this account to top of dashboard")}
        </span>
      </div>
    </div>
  );

  const uriForm = (
    <div className="space-y-2">
      <Field label={t("twofa.uriLabel", "otpauth:// URI")}>
        <Textarea
          rows={4}
          className="font-mono text-xs"
          placeholder="otpauth://totp/GitHub:user?secret=JBSWY3DPEHPK3PXP&issuer=GitHub"
          value={uriInput}
          onChange={(e) => handleParseURI(e.target.value)}
        />
      </Field>
      <p className="text-xs text-muted-foreground">
        {t("twofa.uriHelp", "Paste a provisioning URI to automatically parse the issuer, account, and secret parameters.")}
      </p>
    </div>
  );

  return (
    <Modal
      onClose={onClose}
      title={isEdit ? t("twofa.editAccount", "Edit 2FA Account") : t("twofa.addAccount", "Add 2FA Account")}
    >
      <div className="space-y-4">
        {isEdit ? (
          manualForm
        ) : (
          <Tabs
            value={tab}
            onValueChange={(v) => setTab(v as "manual" | "uri")}
            items={[
              { value: "manual", label: t("twofa.manualEntry", "Manual Entry"), content: manualForm },
              { value: "uri", label: t("twofa.pasteURI", "Paste Key URI (otpauth://)"), content: uriForm },
            ]}
          />
        )}

        {error && <FormError err={error} />}

        <div className="flex justify-end gap-3 border-t border-border pt-4">
          <Button variant="ghost" onClick={onClose} disabled={loading}>
            {t("twofa.cancel", "Cancel")}
          </Button>
          <Button onClick={handleSave} disabled={loading}>
            {loading ? t("twofa.saving", "Saving...") : t("twofa.save", "Save Account")}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
