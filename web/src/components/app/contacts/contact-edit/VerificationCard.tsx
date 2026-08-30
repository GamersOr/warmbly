// The "why" behind a contact's deliverability verdict: an animated
// confidence ring, the reasons in plain words, and the observations they
// were scored from. Absence of engagement is never listed, because it is not
// evidence of anything.

import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    CircleDashedIcon,
    MailCheckIcon,
    MailOpenIcon,
    MailWarningIcon,
    MousePointerClickIcon,
    ReplyIcon,
    ShieldCheckIcon,
    ShieldXIcon,
} from "lucide-react";
import type { ContactVerificationDetail, VerificationEvidenceKind } from "@/lib/api/models/app/contacts/ContactDetail";
import { fmtRelative } from "./format";
import { cn } from "@/lib/utils";

const STATUS = {
    valid: { label: "Deliverable", ring: "stroke-emerald-500", text: "text-emerald-700", Icon: ShieldCheckIcon },
    risky: { label: "Risky", ring: "stroke-amber-500", text: "text-amber-700", Icon: AlertTriangleIcon },
    invalid: { label: "Undeliverable", ring: "stroke-rose-500", text: "text-rose-700", Icon: ShieldXIcon },
    unknown: { label: "Not verified", ring: "stroke-slate-300", text: "text-slate-500", Icon: CircleDashedIcon },
} as const;

const EVIDENCE: Record<VerificationEvidenceKind, { label: string; Icon: typeof MailCheckIcon; tone: string }> = {
    delivered: { label: "Delivered, no bounce", Icon: MailCheckIcon, tone: "text-emerald-600" },
    opened: { label: "Opened by a person", Icon: MailOpenIcon, tone: "text-emerald-600" },
    clicked: { label: "Clicked a link", Icon: MousePointerClickIcon, tone: "text-emerald-600" },
    replied: { label: "Replied", Icon: ReplyIcon, tone: "text-emerald-600" },
    auto_replied: { label: "Automatic reply (mailbox is live)", Icon: ReplyIcon, tone: "text-emerald-600" },
    bounced_recipient: { label: "Bounced: mailbox does not exist", Icon: MailWarningIcon, tone: "text-rose-600" },
    bounced_other: { label: "Bounced for another reason", Icon: MailWarningIcon, tone: "text-slate-500" },
};

export default function VerificationCard({
    detail,
    loading,
}: {
    detail?: ContactVerificationDetail | null;
    loading: boolean;
}) {
    if (loading && !detail) {
        return <div className="h-20 rounded-md border border-slate-200 bg-slate-50 animate-pulse" />;
    }
    if (!detail) return null;
    const meta = STATUS[detail.status] ?? STATUS.unknown;
    const Icon = meta.Icon;
    const r = 16;
    const c = 2 * Math.PI * r;
    const pct = Math.max(0, Math.min(100, detail.confidence));

    return (
        <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
            <div className="px-3 py-2.5 flex items-center gap-3">
                <div className="relative w-11 h-11 shrink-0">
                    <svg viewBox="0 0 40 40" className="w-11 h-11 -rotate-90">
                        <circle cx="20" cy="20" r={r} className="stroke-slate-100" strokeWidth="4" fill="none" />
                        <motion.circle
                            cx="20"
                            cy="20"
                            r={r}
                            className={meta.ring}
                            strokeWidth="4"
                            strokeLinecap="round"
                            fill="none"
                            strokeDasharray={c}
                            initial={{ strokeDashoffset: c }}
                            animate={{ strokeDashoffset: c - (c * pct) / 100 }}
                            transition={{ type: "spring", duration: 1, bounce: 0.15 }}
                        />
                    </svg>
                    <motion.span
                        key={detail.status}
                        initial={{ scale: 0.5, opacity: 0 }}
                        animate={{ scale: 1, opacity: 1 }}
                        transition={{ type: "spring", duration: 0.4, bounce: 0.5 }}
                        className={cn("absolute inset-0 flex items-center justify-center", meta.text)}
                    >
                        <Icon className="w-4 h-4" />
                    </motion.span>
                </div>
                <div className="min-w-0 flex-1">
                    <div className="flex items-baseline gap-1.5">
                        <span className={cn("text-[13px] font-semibold", meta.text)}>{meta.label}</span>
                        <span className="text-[11px] text-slate-400 tabular-nums">{pct}% sure</span>
                        {detail.decisive && (
                            <span className="ml-auto text-[10px] uppercase tracking-[0.12em] text-slate-400 font-medium">
                                from real mail
                            </span>
                        )}
                    </div>
                    <ul className="mt-0.5 space-y-0.5">
                        <AnimatePresence initial={false}>
                            {detail.reasons.slice(0, 3).map((reason, i) => (
                                <motion.li
                                    key={reason}
                                    initial={{ opacity: 0, x: -6 }}
                                    animate={{ opacity: 1, x: 0 }}
                                    transition={{ delay: i * 0.06 }}
                                    className="text-[11.5px] text-slate-600 leading-snug"
                                >
                                    {reason.charAt(0).toUpperCase() + reason.slice(1)}
                                </motion.li>
                            ))}
                        </AnimatePresence>
                    </ul>
                </div>
            </div>
            {detail.evidence.length > 0 && (
                <div className="border-t border-slate-100 divide-y divide-slate-100">
                    {detail.evidence.slice(0, 6).map((e, i) => {
                        const m = EVIDENCE[e.kind] ?? EVIDENCE.bounced_other;
                        const EIcon = m.Icon;
                        return (
                            <motion.div
                                key={`${e.kind}-${e.observed_at}-${i}`}
                                initial={{ opacity: 0 }}
                                animate={{ opacity: 1 }}
                                transition={{ delay: 0.15 + i * 0.05 }}
                                className="px-3 py-1.5 flex items-center gap-2 text-[11.5px]"
                            >
                                <EIcon className={cn("w-3 h-3 shrink-0", m.tone)} />
                                <span className="text-slate-700 truncate">{m.label}</span>
                                <span className="ml-auto text-slate-400 shrink-0">{fmtRelative(e.observed_at)}</span>
                            </motion.div>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
