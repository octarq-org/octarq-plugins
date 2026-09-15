import { useState } from "react";
import { GlassCard, Button, Badge } from "@octarq/plugin-sdk";
import type { AccountSummary } from "../types";

interface Props {
  account: AccountSummary;
  onCopy: (code: string) => void;
  onTogglePin: (id: number) => void;
  onViewSecret: (account: AccountSummary) => void;
  onEdit: (account: AccountSummary) => void;
  onDelete: (id: number) => void;
  t: (key: string, fallback?: string) => string;
}

export function AccountCard({
  account,
  onCopy,
  onTogglePin,
  onViewSecret,
  onEdit,
  onDelete,
  t,
}: Props) {
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
    onCopy(account.current_code);
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
  let strokeColor = "#10b981"; // emerald-500
  if (rem <= 5) {
    strokeColor = "#ef4444"; // red-500
  } else if (rem <= 10) {
    strokeColor = "#f59e0b"; // amber-500
  }

  const tags = account.tags
    ? account.tags.split(",").map((s) => s.trim()).filter(Boolean)
    : [];

  return (
    <GlassCard className="relative flex flex-col justify-between p-5 transition-all duration-200 hover:border-white/20 hover:shadow-lg">
      <div>
        {/* Top bar: Issuer / Title & Pin */}
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="truncate font-semibold text-white">
                {account.name}
              </span>
              {account.pinned && (
                <span className="text-xs text-amber-400" title={t("twofa.pinned", "Pinned")}>
                  📌
                </span>
              )}
            </div>
            {(account.issuer || account.account) && (
              <p className="truncate text-xs text-white/50">
                {account.issuer && <span>{account.issuer}</span>}
                {account.issuer && account.account && <span> • </span>}
                {account.account && <span>{account.account}</span>}
              </p>
            )}
          </div>

          <div className="flex items-center gap-1">
            <button
              onClick={() => onTogglePin(account.id)}
              className="rounded p-1 text-xs text-white/40 hover:bg-white/10 hover:text-white"
              title={account.pinned ? t("twofa.unpin", "Unpin") : t("twofa.pin", "Pin")}
            >
              {account.pinned ? "★" : "☆"}
            </button>
          </div>
        </div>

        {/* Live TOTP Code display & countdown circle */}
        <div className="my-5 flex items-center justify-between rounded-lg bg-black/25 px-4 py-3 border border-white/5">
          <div
            onClick={handleCopy}
            className="group cursor-pointer select-all font-mono text-2xl font-bold tracking-widest text-white transition-colors hover:text-emerald-400"
            title={t("twofa.clickToCopy", "Click to copy code")}
          >
            {formatCode(account.current_code)}
          </div>

          {/* Radial Countdown Timer */}
          <div className="flex items-center gap-3">
            <div className="relative flex h-8 w-8 items-center justify-center">
              <svg className="h-8 w-8 -rotate-90 transform" viewBox="0 0 36 36">
                <circle
                  cx="18"
                  cy="18"
                  r={radius}
                  stroke="rgba(255, 255, 255, 0.1)"
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
              <span className="absolute text-[10px] font-medium text-white/70">
                {rem}
              </span>
            </div>

            <Button
              variant={copied ? "subtle" : "ghost"}
              onClick={handleCopy}
              className="h-8 px-2 text-xs"
            >
              {copied ? t("twofa.copied", "Copied!") : t("twofa.copy", "Copy")}
            </Button>
          </div>
        </div>

        {/* Tags */}
        {tags.length > 0 && (
          <div className="mb-3 flex flex-wrap gap-1">
            {tags.map((tg) => (
              <Badge key={tg} tone="neutral" className="text-[10px] px-1.5 py-0.5">
                {tg}
              </Badge>
            ))}
          </div>
        )}

        {/* Notes preview */}
        {account.notes && (
          <p className="line-clamp-2 text-xs text-white/40 italic mb-3">
            {account.notes}
          </p>
        )}
      </div>

      {/* Card Footer Actions */}
      <div className="flex items-center justify-between border-t border-white/5 pt-3 text-xs text-white/40">
        <span className="text-[11px]">
          {account.algorithm} • {account.digits}d • {account.period}s
        </span>
        <div className="flex items-center gap-2">
          <button
            onClick={() => onViewSecret(account)}
            className="rounded p-1 hover:bg-white/10 hover:text-white"
            title={t("twofa.showSecret", "Reveal Secret & QR")}
          >
            🔍 QR
          </button>
          <button
            onClick={() => onEdit(account)}
            className="rounded p-1 hover:bg-white/10 hover:text-white"
            title={t("twofa.edit", "Edit")}
          >
            ✏️
          </button>
          <button
            onClick={() => onDelete(account.id)}
            className="rounded p-1 hover:bg-red-500/20 hover:text-red-400"
            title={t("twofa.delete", "Delete")}
          >
            🗑️
          </button>
        </div>
      </div>
    </GlassCard>
  );
}
