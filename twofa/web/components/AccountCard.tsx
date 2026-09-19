import { useState } from "react";
import { GlassCard, Button, Badge, useToast } from "@octarq/plugin-sdk";
import { Copy, Check, Pencil, Pin, QrCode, Trash2 } from "lucide-react";
import type { AccountSummary } from "../types";

interface Props {
  account: AccountSummary;
  onTogglePin: (id: number) => void;
  onViewSecret: (account: AccountSummary) => void;
  onEdit: (account: AccountSummary) => void;
  onDelete: (id: number) => void;
  t: (key: string, fallback?: string) => string;
}

export function AccountCard({
  account,
  onTogglePin,
  onViewSecret,
  onEdit,
  onDelete,
  t,
}: Props) {
  const toast = useToast();
  const [copied, setCopied] = useState(false);

  // Format code with a space in the middle for readability (e.g. "123 456")
  const formatCode = (code?: string) => {
    if (!code) return "••••••";
    if (code.length === 6) {
      return `${code.slice(0, 3)} ${code.slice(3)}`;
    }
    if (code.length === 8) {
      return `${code.slice(0, 4)} ${code.slice(4)}`;
    }
    return code;
  };

  const handleCopy = () => {
    if (!account.current_code) return;
    navigator.clipboard.writeText(account.current_code);
    toast.success(t("twofa.copiedToast", "Code copied to clipboard!"));
    setCopied(true);
    setTimeout(() => setCopied(false), 1800);
  };

  // Calculate SVG circular countdown ring
  const period = account.period || 30;
  const rem = Math.max(0, Math.min(period, account.seconds_remaining));
  const progress = rem / period;
  const radius = 14;
  const circumference = 2 * Math.PI * radius;
  const strokeDashoffset = circumference * (1 - progress);

  // Dynamic progress color based on remaining urgency
  let strokeColor = "var(--success-fg, #10b981)";
  if (rem <= 5) {
    strokeColor = "var(--danger-fg, #ef4444)";
  } else if (rem <= 10) {
    strokeColor = "var(--warning-fg, #f59e0b)";
  }

  const tags = account.tags
    ? account.tags.split(",").map((s) => s.trim()).filter(Boolean)
    : [];

  return (
    <GlassCard className="relative flex flex-col justify-between p-5 transition-all duration-200">
      <div>
        {/* Top bar: title, issuer/account & pin */}
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="truncate font-semibold text-foreground">{account.name}</span>
              {account.pinned && (
                <span title={t("twofa.pinned", "Pinned")} className="shrink-0">
                  <Pin className="h-3.5 w-3.5 text-warning-fg" />
                </span>
              )}
            </div>
            {(account.issuer || account.account) && (
              <p className="truncate text-xs text-muted-foreground">
                {account.issuer && <span>{account.issuer}</span>}
                {account.issuer && account.account && <span> • </span>}
                {account.account && <span>{account.account}</span>}
              </p>
            )}
          </div>

          <Button
            variant="ghost"
            size="sm"
            onClick={() => onTogglePin(account.id)}
            title={account.pinned ? t("twofa.unpin", "Unpin") : t("twofa.pin", "Pin")}
            aria-pressed={account.pinned}
            className="h-7 w-7 p-0"
          >
            <Pin className={account.pinned ? "h-4 w-4 text-warning-fg" : "h-4 w-4"} />
          </Button>
        </div>

        {/* Live TOTP code & countdown circle */}
        <div className="my-5 flex items-center justify-between rounded-lg border border-border bg-well px-4 py-3">
          <button
            type="button"
            onClick={handleCopy}
            className="select-all font-mono text-2xl font-bold tracking-widest text-foreground transition-colors hover:text-accent-fg"
            title={t("twofa.clickToCopy", "Click to copy code")}
          >
            {formatCode(account.current_code)}
          </button>

          {/* Radial countdown timer */}
          <div className="flex items-center gap-3">
            <div className="relative flex h-8 w-8 items-center justify-center">
              <svg className="h-8 w-8 -rotate-90 transform" viewBox="0 0 36 36">
                <circle
                  cx="18"
                  cy="18"
                  r={radius}
                  stroke="currentColor"
                  className="text-foreground/10"
                  strokeWidth="3"
                  fill="none"
                />
                <circle
                  cx="18"
                  cy="18"
                  r={radius}
                  stroke={strokeColor}
                  strokeWidth="3"
                  fill="none"
                  strokeDasharray={circumference}
                  strokeDashoffset={strokeDashoffset}
                  strokeLinecap="round"
                  className="transition-all duration-500 ease-linear"
                />
              </svg>
              <span className="absolute text-[10px] font-medium text-muted-foreground">{rem}</span>
            </div>

            <Button
              variant={copied ? "subtle" : "outline"}
              size="sm"
              onClick={handleCopy}
            >
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {copied ? t("twofa.copied", "Copied!") : t("twofa.copy", "Copy")}
            </Button>
          </div>
        </div>

        {/* Tags */}
        {tags.length > 0 && (
          <div className="mb-3 flex flex-wrap gap-1">
            {tags.map((tg) => (
              <Badge key={tg} tone="neutral" className="px-1.5 py-0.5 text-[10px]">
                {tg}
              </Badge>
            ))}
          </div>
        )}

        {/* Notes preview */}
        {account.notes && (
          <p className="mb-3 line-clamp-2 text-xs italic text-muted-foreground">{account.notes}</p>
        )}
      </div>

      {/* Card footer actions */}
      <div className="flex items-center justify-between border-t border-border pt-3 text-xs text-muted-foreground">
        <span className="text-[11px]">
          {account.algorithm} • {account.digits}d • {account.period}s
        </span>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onViewSecret(account)}
            title={t("twofa.showSecret", "Reveal Secret & QR")}
            className="h-7 px-2"
          >
            <QrCode className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onEdit(account)}
            title={t("twofa.edit", "Edit")}
            className="h-7 px-2"
          >
            <Pencil className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="danger"
            size="sm"
            onClick={() => onDelete(account.id)}
            title={t("twofa.delete", "Delete")}
            className="h-7 px-2"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>
    </GlassCard>
  );
}
