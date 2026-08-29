// The three-step link flow: link this instance to a Warmbly Cloud workspace,
// pick the mailboxes the cloud should warm, done. Steps slide directionally
// like NewCampaignDialog; a step cannot be skipped ahead of.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import {
    ArrowRightIcon,
    CheckIcon,
    CloudIcon,
    CopyIcon,
    ExternalLinkIcon,
    FlameIcon,
    InboxIcon,
    Loader2Icon,
    LockIcon,
    ShieldCheckIcon,
    SparklesIcon,
} from "lucide-react";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type { CloudLinkMailboxRow, CloudLinkPendingConnect, CloudLinkStatus } from "@/lib/api/models/app/cloudlink/CloudLink";
import {
    useCloudLinkMailboxes,
    useEnrollCloudLinkMailbox,
    usePollCloudLinkConnect,
    useStartCloudLinkConnect,
    useUnenrollCloudLinkMailbox,
} from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import { Toggle } from "../_components/SectionShell";
import { providerLabel, providerSupported } from "./providers";

const STEPS = [
    { label: "Link", icon: CloudIcon },
    { label: "Mailboxes", icon: InboxIcon },
    { label: "Done", icon: SparklesIcon },
] as const;
type Step = 0 | 1 | 2;

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 28, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -28, opacity: 0 }),
};

export default function ConnectFlow({
    status,
    onFinished,
}: {
    status: CloudLinkStatus;
    onFinished: () => void;
}) {
    const [step, setStep] = React.useState<Step>(status.connected ? 1 : 0);
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [nudged, setNudged] = React.useState(false);
    const [linked, setLinked] = React.useState(status.connected);
    const [enrolledCount, setEnrolledCount] = React.useState(0);

    const issue = step === 0 && !linked ? "Approve the code on Warmbly Cloud first" : null;
    React.useEffect(() => {
        if (!issue) setNudged(false);
    }, [issue]);

    const canReach = React.useCallback((target: Step) => (target === 0 ? true : linked), [linked]);
    const goTo = React.useCallback(
        (target: Step) => {
            if (target === step) return;
            if (target > step && !canReach(target)) {
                setNudged(true);
                return;
            }
            setDirection(target > step ? 1 : -1);
            setNudged(false);
            setStep(target);
        },
        [step, canReach],
    );

    return (
        <div className="px-4 py-5 md:px-8 md:py-6">
            <div className="rounded-lg border border-slate-200 bg-white overflow-hidden">
                <Stepper step={step} canReach={canReach} goTo={goTo} />
                <div className="overflow-hidden">
                    <AnimatePresence mode="wait" initial={false} custom={direction}>
                        <motion.div
                            key={step}
                            custom={direction}
                            variants={paneVariants}
                            initial="enter"
                            animate="center"
                            exit="exit"
                            transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                            className="px-5 py-5 min-h-[320px]"
                        >
                            {step === 0 && (
                                <LinkStep
                                    status={status}
                                    linked={linked}
                                    onLinked={() => {
                                        setLinked(true);
                                        setTimeout(() => goTo(1), 650);
                                    }}
                                />
                            )}
                            {step === 1 && <MailboxesStep onCountChange={setEnrolledCount} />}
                            {step === 2 && <DoneStep status={status} enrolledCount={enrolledCount} />}
                        </motion.div>
                    </AnimatePresence>
                </div>
                <Footer
                    step={step}
                    issue={issue}
                    nudged={nudged}
                    onBack={() => goTo((step - 1) as Step)}
                    onNext={() => {
                        if (issue) {
                            setNudged(true);
                            return;
                        }
                        if (step < 2) goTo((step + 1) as Step);
                        else onFinished();
                    }}
                />
            </div>
        </div>
    );
}

function Stepper({ step, canReach, goTo }: { step: Step; canReach: (s: Step) => boolean; goTo: (s: Step) => void }) {
    return (
        <div className="px-5 pt-4 pb-3 border-b border-slate-200/70 flex items-center gap-2">
            {STEPS.map((s, idx) => {
                const i = idx as Step;
                const done = i < step;
                const active = i === step;
                const reachable = i <= step || canReach(i);
                return (
                    <React.Fragment key={s.label}>
                        <button
                            type="button"
                            disabled={!reachable}
                            onClick={() => goTo(i)}
                            className={`inline-flex items-center gap-1.5 text-[12px] transition-colors ${
                                active ? "text-slate-900 font-medium" : done ? "text-slate-700" : "text-slate-400"
                            } ${reachable ? "" : "cursor-not-allowed"}`}
                        >
                            <span
                                className={`relative size-5 rounded-full inline-flex items-center justify-center text-[10px] border transition-colors ${
                                    done ? "bg-sky-600 border-sky-600 text-white" : active ? "border-sky-600 text-sky-700" : "border-slate-300 text-slate-400"
                                }`}
                            >
                                <AnimatePresence mode="wait" initial={false}>
                                    {done ? (
                                        <motion.span key="check" initial={{ scale: 0.4, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} exit={{ scale: 0.4, opacity: 0 }}>
                                            <CheckIcon className="w-3 h-3" />
                                        </motion.span>
                                    ) : (
                                        <motion.span key="num" initial={{ scale: 0.4, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} exit={{ scale: 0.4, opacity: 0 }}>
                                            {i + 1}
                                        </motion.span>
                                    )}
                                </AnimatePresence>
                            </span>
                            {s.label}
                        </button>
                        {idx < STEPS.length - 1 && (
                            <span className="relative flex-1 h-px bg-slate-200 overflow-hidden">
                                <motion.span
                                    className="absolute inset-0 bg-sky-500"
                                    style={{ originX: 0 }}
                                    animate={{ scaleX: done ? 1 : 0 }}
                                    transition={{ duration: 0.3, ease: [0.22, 1, 0.36, 1] }}
                                />
                            </span>
                        )}
                    </React.Fragment>
                );
            })}
        </div>
    );
}

function Footer({ step, issue, nudged, onBack, onNext }: { step: Step; issue: string | null; nudged: boolean; onBack: () => void; onNext: () => void }) {
    return (
        <div className="px-5 py-3 border-t border-slate-200/70 flex items-center gap-3">
            <div className="flex-1 min-w-0">
                <AnimatePresence>
                    {nudged && issue && (
                        <motion.p
                            role="status"
                            initial={{ opacity: 0, y: 4 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: 4 }}
                            className="text-[12px] text-amber-700"
                        >
                            {issue}
                        </motion.p>
                    )}
                </AnimatePresence>
            </div>
            {step > 0 && (
                <button type="button" onClick={onBack} className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors">
                    Back
                </button>
            )}
            <button
                type="button"
                onClick={onNext}
                className={`h-7 px-2.5 rounded-md text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors ${
                    issue ? "bg-slate-300 cursor-not-allowed" : "bg-sky-600 hover:bg-sky-700"
                }`}
            >
                {step === 2 ? "Finish" : "Continue"}
                <ArrowRightIcon className="w-3.5 h-3.5" />
            </button>
        </div>
    );
}

// Step 1: get a code, send the user to Warmbly Cloud, wait for approval.
function LinkStep({ status, linked, onLinked }: { status: CloudLinkStatus; linked: boolean; onLinked: () => void }) {
    const start = useStartCloudLinkConnect();
    const poll = usePollCloudLinkConnect();
    const [pending, setPending] = React.useState<CloudLinkPendingConnect | null>(null);
    const [copied, setCopied] = React.useState(false);
    const [orgName, setOrgName] = React.useState(status.link?.organization_name ?? "");

    const begin = async () => {
        try {
            const p = await start.mutateAsync(undefined);
            setPending(p);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    // Poll on the cloud's interval while a code is out.
    React.useEffect(() => {
        if (!pending || linked) return;
        let stopped = false;
        const tick = async () => {
            if (stopped) return;
            try {
                const res = await poll.mutateAsync();
                if (res.status === "approved") {
                    setOrgName(res.link?.organization_name ?? res.info?.organization.name ?? "");
                    onLinked();
                    return;
                }
            } catch (e) {
                const err = e as AppError;
                if (err.code === "cloud_link_code_expired" || err.code === "pool_link_denied" || err.code === "pool_link_code_not_found") {
                    toast.error(err.message);
                    setPending(null);
                    return;
                }
            }
            if (!stopped) window.setTimeout(tick, Math.max(2, pending.interval) * 1000);
        };
        const t = window.setTimeout(tick, Math.max(2, pending.interval) * 1000);
        return () => {
            stopped = true;
            window.clearTimeout(t);
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [pending, linked]);

    const copy = async () => {
        if (!pending) return;
        try {
            await navigator.clipboard.writeText(pending.user_code);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
        } catch {
            toast.error("Could not copy");
        }
    };

    if (linked) {
        return (
            <motion.div initial={{ opacity: 0, scale: 0.98 }} animate={{ opacity: 1, scale: 1 }} className="h-full flex flex-col items-center justify-center text-center gap-3 py-8">
                <motion.span
                    initial={{ scale: 0.6 }}
                    animate={{ scale: 1 }}
                    transition={{ type: "spring", stiffness: 420, damping: 22 }}
                    className="size-12 rounded-full bg-emerald-50 text-emerald-600 inline-flex items-center justify-center"
                >
                    <CheckIcon className="w-6 h-6" />
                </motion.span>
                <div>
                    <p className="text-[14px] font-semibold text-slate-900">Linked{orgName ? ` to ${orgName}` : ""}</p>
                    <p className="text-[12.5px] text-slate-500 mt-0.5">This instance can now use the Warmbly warmup pool.</p>
                </div>
            </motion.div>
        );
    }

    if (!pending) {
        return (
            <div className="grid md:grid-cols-[1fr_260px] gap-6 items-start">
                <div className="space-y-4">
                    <div>
                        <h3 className="text-[14px] font-semibold text-slate-900">Warm your mailboxes with the Warmbly pool</h3>
                        <p className="text-[12.5px] text-slate-500 leading-relaxed mt-1">
                            Link this instance to a Warmbly Cloud workspace. Warmbly runs warmup for the mailboxes you choose, using thousands of real
                            mailboxes, while everything else stays on this server.
                        </p>
                    </div>
                    <ul className="space-y-2 text-[12.5px] text-slate-600">
                        <Bullet icon={FlameIcon}>Free for up to 10 mailboxes. Unlimited mailboxes for $15 a month.</Bullet>
                        <Bullet icon={LockIcon}>Only the mailbox credential travels, encrypted, for warmup. Campaigns, contacts and inbox never leave here.</Bullet>
                        <Bullet icon={ShieldCheckIcon}>You can unenroll a mailbox or disconnect at any time; the cloud forgets it immediately.</Bullet>
                    </ul>
                    <button
                        type="button"
                        onClick={begin}
                        disabled={start.isPending}
                        className="h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {start.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CloudIcon className="w-3.5 h-3.5" />}
                        Connect to Warmbly Cloud
                    </button>
                    <p className="text-[11px] text-slate-400">Connecting to {status.default_cloud_url}</p>
                </div>
                <Illustration />
            </div>
        );
    }

    return (
        <div className="flex flex-col items-center text-center gap-4 py-2">
            <p className="text-[12.5px] text-slate-500">Enter this code on Warmbly Cloud to approve the link.</p>
            <motion.button
                type="button"
                onClick={copy}
                initial={{ scale: 0.96, opacity: 0 }}
                animate={{ scale: 1, opacity: 1 }}
                className="group relative rounded-lg border border-slate-200 bg-slate-50 px-6 py-4 font-mono text-[28px] tracking-[0.28em] text-slate-900 hover:border-sky-300 transition-colors"
                title="Copy code"
            >
                {pending.user_code}
                <span className="absolute -top-2 -right-2 size-6 rounded-full bg-white border border-slate-200 text-slate-500 inline-flex items-center justify-center group-hover:text-sky-600">
                    {copied ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <CopyIcon className="w-3 h-3" />}
                </span>
            </motion.button>
            <a
                href={pending.verification_url}
                target="_blank"
                rel="noreferrer"
                className="h-8 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors"
            >
                Open Warmbly Cloud
                <ExternalLinkIcon className="w-3.5 h-3.5" />
            </a>
            <div className="inline-flex items-center gap-2 text-[12px] text-slate-500">
                <span className="relative flex size-2">
                    <span className="absolute inline-flex h-full w-full rounded-full bg-sky-400 opacity-75 animate-ping" />
                    <span className="relative inline-flex size-2 rounded-full bg-sky-500" />
                </span>
                Waiting for approval
                <Countdown until={pending.expires_at} />
            </div>
        </div>
    );
}

function Countdown({ until }: { until: Date }) {
    const [left, setLeft] = React.useState(() => Math.max(0, Math.floor((new Date(until).getTime() - Date.now()) / 1000)));
    React.useEffect(() => {
        const t = window.setInterval(() => setLeft(Math.max(0, Math.floor((new Date(until).getTime() - Date.now()) / 1000))), 1000);
        return () => window.clearInterval(t);
    }, [until]);
    const m = Math.floor(left / 60);
    const s = left % 60;
    return (
        <span className="tabular-nums text-slate-400">
            · code expires in {m}:{s.toString().padStart(2, "0")}
        </span>
    );
}

function Bullet({ icon: Icon, children }: { icon: React.ComponentType<{ className?: string }>; children: React.ReactNode }) {
    return (
        <li className="flex items-start gap-2">
            <Icon className="w-3.5 h-3.5 mt-0.5 text-sky-600 shrink-0" />
            <span className="leading-relaxed">{children}</span>
        </li>
    );
}

// A small animated diagram: this server, the cloud, and mail flowing between.
function Illustration() {
    return (
        <div className="rounded-lg border border-slate-200 bg-gradient-to-b from-sky-50/60 to-white p-4 h-[170px] relative overflow-hidden">
            <div className="absolute left-5 top-1/2 -translate-y-1/2 flex flex-col items-center gap-1.5">
                <span className="size-10 rounded-md bg-white border border-slate-200 inline-flex items-center justify-center text-slate-700">
                    <InboxIcon className="w-4 h-4" />
                </span>
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">You</span>
            </div>
            <div className="absolute right-5 top-1/2 -translate-y-1/2 flex flex-col items-center gap-1.5">
                <span className="size-10 rounded-md bg-sky-600 inline-flex items-center justify-center text-white">
                    <CloudIcon className="w-4 h-4" />
                </span>
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Pool</span>
            </div>
            <div className="absolute left-[72px] right-[72px] top-1/2 -translate-y-1/2 h-px bg-slate-200" />
            {[0, 1, 2].map((i) => (
                <motion.span
                    key={i}
                    className="absolute top-1/2 -translate-y-1/2 size-1.5 rounded-full bg-sky-500"
                    initial={{ left: 72, opacity: 0 }}
                    animate={{ left: ["72px", "calc(100% - 78px)"], opacity: [0, 1, 1, 0] }}
                    transition={{ duration: 2.4, delay: i * 0.8, repeat: Infinity, ease: "easeInOut" }}
                />
            ))}
        </div>
    );
}

// Step 2: pick mailboxes.
function MailboxesStep({ onCountChange }: { onCountChange: (n: number) => void }) {
    const rows = useCloudLinkMailboxes();
    const enroll = useEnrollCloudLinkMailbox();
    const unenroll = useUnenrollCloudLinkMailbox();
    const [busy, setBusy] = React.useState<string | null>(null);

    React.useEffect(() => {
        onCountChange((rows.data ?? []).filter((r) => r.enrolled).length);
    }, [rows.data, onCountChange]);

    const flip = async (row: CloudLinkMailboxRow) => {
        setBusy(row.id);
        try {
            if (row.enrolled) {
                await unenroll.mutateAsync(row.id);
                toast.success(`${row.email} removed from the pool`);
            } else {
                await enroll.mutateAsync(row.id);
                toast.success(`${row.email} is now warming in the pool`);
            }
        } catch (e) {
            toast.error(buildError(e as AppError));
        } finally {
            setBusy(null);
        }
    };

    return (
        <div className="space-y-4">
            <div>
                <h3 className="text-[14px] font-semibold text-slate-900">Choose the mailboxes to warm</h3>
                <p className="text-[12.5px] text-slate-500 leading-relaxed mt-1">
                    Each enrolled mailbox is warmed by Warmbly Cloud from now on. Its local warmup stops; campaigns keep sending from here as usual.
                </p>
            </div>
            {rows.isLoading ? (
                <div className="py-8 flex justify-center text-slate-400">
                    <Loader2Icon className="w-4 h-4 animate-spin" />
                </div>
            ) : (rows.data ?? []).length === 0 ? (
                <p className="text-[12.5px] text-slate-500">No active mailboxes yet. Connect one under Mailboxes, then come back here.</p>
            ) : (
                <ul className="rounded-md border border-slate-200 divide-y divide-slate-200/70 overflow-hidden">
                    {(rows.data ?? []).map((row, i) => {
                        const supported = providerSupported(row.provider);
                        return (
                            <motion.li
                                key={row.id}
                                initial={{ opacity: 0, y: 6 }}
                                animate={{ opacity: 1, y: 0 }}
                                transition={{ delay: i * 0.03 }}
                                className="flex items-center gap-3 px-3 h-11 bg-white"
                            >
                                <div className="min-w-0 flex-1">
                                    <p className="text-[12.5px] text-slate-900 truncate">{row.email}</p>
                                    <p className="text-[11px] text-slate-400 truncate">
                                        {providerLabel(row.provider)}
                                        {!supported && " · connect with SMTP/IMAP to enroll"}
                                    </p>
                                </div>
                                {busy === row.id ? (
                                    <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400" />
                                ) : (
                                    <Toggle on={row.enrolled} disabled={!supported} onChange={() => void flip(row)} />
                                )}
                            </motion.li>
                        );
                    })}
                </ul>
            )}
            <p className="text-[11px] text-slate-400 leading-relaxed">
                Enrolling sends the mailbox's SMTP/IMAP credential to Warmbly Cloud, sealed in transit and at rest, and used only to send and read warmup mail.
                Nothing else in the mailbox is stored.
            </p>
        </div>
    );
}

function DoneStep({ status, enrolledCount }: { status: CloudLinkStatus; enrolledCount: number }) {
    return (
        <div className="flex flex-col items-center justify-center text-center gap-3 py-8">
            <motion.span
                initial={{ scale: 0.6, rotate: -12 }}
                animate={{ scale: 1, rotate: 0 }}
                transition={{ type: "spring", stiffness: 380, damping: 20 }}
                className="size-12 rounded-full bg-sky-50 text-sky-600 inline-flex items-center justify-center"
            >
                <FlameIcon className="w-6 h-6" />
            </motion.span>
            <div>
                <p className="text-[14px] font-semibold text-slate-900">
                    {enrolledCount === 0 ? "You are all set" : `${enrolledCount} mailbox${enrolledCount === 1 ? "" : "es"} warming in the pool`}
                </p>
                <p className="text-[12.5px] text-slate-500 mt-0.5 max-w-md">
                    {status.link?.organization_name ? `Linked to ${status.link.organization_name}. ` : ""}
                    Warmup starts on the first slot of each mailbox's window and ramps daily. Health and volume show up on this page as they arrive.
                </p>
            </div>
        </div>
    );
}
