// Advisory campaign-template content check: scores the current subject + body
// against /templates/score and renders a 0-100 score (higher = safer) plus the
// non-blocking issues found, re-scored on the debounce the composer's preview
// uses. It never blocks saving or sending.

import * as React from "react";
import { ShieldCheckIcon, AlertTriangleIcon, AlertCircleIcon } from "lucide-react";
import scoreTemplate from "@/lib/api/client/app/campaigns/scoreTemplate";
import type TemplateScore from "@/lib/api/models/app/campaigns/TemplateScore";
import type { TemplateScoreIssue } from "@/lib/api/models/app/campaigns/TemplateScore";
import { Loading } from "@/components/loader";
import { cn } from "@/lib/utils";
import { DitherMeter, type DitherTone } from "@/components/ui/dither";

function scoreTone(score: number) {
    if (score >= 80) return { text: "text-emerald-600", meter: "emerald" as DitherTone, label: "Looks good" };
    if (score >= 50) return { text: "text-amber-600", meter: "amber" as DitherTone, label: "Could improve" };
    return { text: "text-rose-600", meter: "rose" as DitherTone, label: "Needs work" };
}

function IssueRow({ issue }: { issue: TemplateScoreIssue }) {
    const high = issue.severity === "high";
    const Icon = high ? AlertCircleIcon : AlertTriangleIcon;
    return (
        <li className="flex items-start gap-2 py-1.5">
            <Icon className={cn("w-3.5 h-3.5 shrink-0 mt-0.5", high ? "text-rose-500" : "text-amber-500")} />
            <div className="min-w-0">
                <span className="text-[12px] text-slate-700 leading-relaxed">{issue.message}</span>
                <span className="ml-1.5 text-[10px] text-slate-400 font-mono">{issue.code}</span>
            </div>
        </li>
    );
}

export default function ContentScore({
    subject,
    bodyHtml,
    bodyPlain,
}: {
    subject: string;
    bodyHtml: string;
    bodyPlain: string;
}) {
    const [data, setData] = React.useState<TemplateScore | null>(null);
    const [pending, setPending] = React.useState(false);
    const [failed, setFailed] = React.useState(false);

    React.useEffect(() => {
        // A step with nothing written yet is not a content problem, so hold the
        // panel quiet rather than scoring an empty draft as spam.
        if (!subject.trim() && !bodyPlain.trim()) {
            // Clears pending too: a cancelled request can no longer do it.
            setData(null);
            setPending(false);
            setFailed(false);
            return;
        }
        let cancelled = false;
        // Set inside the timer so the spinner marks a request, not a keystroke.
        const t = setTimeout(() => {
            setPending(true);
            scoreTemplate({ subject, body_html: bodyHtml, body_plain: bodyPlain })
                .then((res) => {
                    if (cancelled) return;
                    setData(res);
                    setFailed(false);
                })
                .catch(() => {
                    if (!cancelled) setFailed(true);
                })
                .finally(() => {
                    if (!cancelled) setPending(false);
                });
        }, 600);
        return () => {
            cancelled = true;
            clearTimeout(t);
        };
    }, [subject, bodyHtml, bodyPlain]);

    const tone = data ? scoreTone(data.score) : null;

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            <div className="flex items-center justify-between gap-3 px-3 py-2.5">
                <div className="min-w-0">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Content check</div>
                    <p className="mt-0.5 text-[11px] text-slate-400 leading-relaxed">Advisory deliverability score. It never blocks sending.</p>
                </div>
                {pending && <Loading className="!w-3.5 h-3.5 shrink-0" />}
            </div>

            {failed && (
                <div className="px-3 pb-3 text-[11.5px] text-rose-600">Couldn&apos;t score this template.</div>
            )}

            {data && tone && (
                <div className="px-3 pb-3 border-t border-slate-200/60 pt-3">
                    <div className="flex items-center gap-2">
                        <span className={cn("text-[22px] font-light leading-none tabular-nums", tone.text)}>{data.score}</span>
                        <span className="text-[11px] text-slate-400 mb-0.5">/ 100</span>
                        <span className={cn("ml-auto text-[11px] font-medium", tone.text)}>{tone.label}</span>
                    </div>
                    <DitherMeter
                        frac={Math.max(0, Math.min(100, data.score)) / 100}
                        tone={tone.meter}
                        height={6}
                        className="mt-2"
                    />
                    {data.issues.length > 0 ? (
                        <ul className="mt-2 divide-y divide-slate-200/60">
                            {data.issues.map((issue, i) => (
                                <IssueRow key={`${issue.code}-${i}`} issue={issue} />
                            ))}
                        </ul>
                    ) : (
                        <p className="mt-2 inline-flex items-center gap-1.5 text-[11.5px] text-emerald-600">
                            <ShieldCheckIcon className="w-3.5 h-3.5" /> No content issues found.
                        </p>
                    )}
                </div>
            )}
        </div>
    );
}
