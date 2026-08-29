// app.warmbly.com/connect?code=XXXX-XXXX — a signed-in member approves a
// self-hosted instance's request to use this workspace's warmup pool.
// Standalone like the OAuth consent page: no dashboard chrome.

import React from "react";
import { Navigate, useSearchParams } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import { CheckIcon, CloudIcon, Loader2Icon, ServerIcon, ShieldCheckIcon } from "lucide-react";
import { Logo } from "@/components/svg";
import getToken from "@/lib/helper/getToken";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import useOrganizations from "@/lib/api/hooks/app/organizations/useOrganizations";
import { useApprovePoolLinkCode, useDenyPoolLinkCode, usePoolLinkCode } from "@/lib/api/hooks/app/cloudlink/useCloudLink";

function normalize(raw: string): string {
    const s = raw.toUpperCase().replace(/[^A-Z0-9]/g, "").slice(0, 8);
    return s.length > 4 ? `${s.slice(0, 4)}-${s.slice(4)}` : s;
}

export default function ConnectPage() {
    if (!getToken()) {
        const next = encodeURIComponent(window.location.pathname + window.location.search);
        return <Navigate to={`/auth/login?next=${next}`} replace />;
    }
    return <ConnectInner />;
}

function ConnectInner() {
    const [params] = useSearchParams();
    const [code, setCode] = React.useState(() => normalize(params.get("code") ?? ""));
    const lookup = usePoolLinkCode(code);
    const orgs = useOrganizations();
    const approve = useApprovePoolLinkCode();
    const deny = useDenyPoolLinkCode();
    const [orgId, setOrgId] = React.useState("");
    const [outcome, setOutcome] = React.useState<"approved" | "denied" | null>(null);

    React.useEffect(() => {
        if (!orgId && orgs.data && orgs.data.length > 0) setOrgId(orgs.data[0].id);
    }, [orgs.data, orgId]);

    const complete = code.replace("-", "").length === 8;
    const pending = lookup.data?.status === "pending";

    const doApprove = async () => {
        try {
            await approve.mutateAsync({ code, organizationId: orgId });
            setOutcome("approved");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };
    const doDeny = async () => {
        try {
            await deny.mutateAsync(code);
            setOutcome("denied");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <div className="min-h-screen flex items-center justify-center bg-slate-50 px-4">
            <div className="w-full max-w-md">
                <div className="flex justify-center mb-6">
                    <Logo className="h-7 w-auto" />
                </div>
                <motion.div
                    initial={{ y: 8, opacity: 0 }}
                    animate={{ y: 0, opacity: 1 }}
                    className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm overflow-hidden"
                >
                    <AnimatePresence mode="wait" initial={false}>
                        {outcome ? (
                            <motion.div key="done" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} className="flex flex-col items-center text-center gap-3 py-4">
                                <motion.span
                                    initial={{ scale: 0.6 }}
                                    animate={{ scale: 1 }}
                                    transition={{ type: "spring", stiffness: 420, damping: 22 }}
                                    className={`size-12 rounded-full inline-flex items-center justify-center ${
                                        outcome === "approved" ? "bg-emerald-50 text-emerald-600" : "bg-slate-100 text-slate-500"
                                    }`}
                                >
                                    <CheckIcon className="w-6 h-6" />
                                </motion.span>
                                <p className="text-[15px] font-semibold text-slate-900">{outcome === "approved" ? "Instance linked" : "Request declined"}</p>
                                <p className="text-[12.5px] text-slate-500 leading-relaxed">
                                    {outcome === "approved"
                                        ? "You can close this tab. Your instance picks the link up on its own within a few seconds."
                                        : "Nothing was linked. You can close this tab."}
                                </p>
                            </motion.div>
                        ) : (
                            <motion.div key="form" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0, y: -8 }} className="space-y-5">
                                <div className="flex items-start gap-3">
                                    <span className="size-9 rounded-md bg-sky-600 text-white inline-flex items-center justify-center shrink-0">
                                        <CloudIcon className="w-4 h-4" />
                                    </span>
                                    <div>
                                        <h1 className="text-[15px] font-semibold text-slate-900">Link a self-hosted instance</h1>
                                        <p className="text-[12.5px] text-slate-500 leading-relaxed mt-0.5">
                                            Enter the code shown in your instance's Warmbly Cloud settings.
                                        </p>
                                    </div>
                                </div>

                                <div>
                                    <label className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium block mb-1.5">Code</label>
                                    <input
                                        value={code}
                                        onChange={(e) => setCode(normalize(e.target.value))}
                                        autoFocus
                                        spellCheck={false}
                                        autoComplete="off"
                                        placeholder="ABCD-EFGH"
                                        className="w-full h-11 rounded-md border border-slate-200 bg-white font-mono text-[20px] tracking-[0.22em] text-center text-slate-900 placeholder:text-slate-300 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                    />
                                </div>

                                <AnimatePresence mode="wait" initial={false}>
                                    {complete && lookup.isLoading && (
                                        <motion.div key="loading" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="flex justify-center text-slate-400 py-2">
                                            <Loader2Icon className="w-4 h-4 animate-spin" />
                                        </motion.div>
                                    )}
                                    {complete && lookup.isError && (
                                        <motion.p key="err" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="text-[12.5px] text-rose-600">
                                            {(lookup.error as unknown as AppError)?.message ?? "That code is unknown or has expired."}
                                        </motion.p>
                                    )}
                                    {lookup.data && (
                                        <motion.div key="details" initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0 }} className="space-y-4">
                                            <div className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2.5 flex items-center gap-3">
                                                <span className="size-7 rounded-md bg-white border border-slate-200 text-slate-600 inline-flex items-center justify-center shrink-0">
                                                    <ServerIcon className="w-3.5 h-3.5" />
                                                </span>
                                                <div className="min-w-0">
                                                    <p className="text-[12.5px] text-slate-900 truncate">{lookup.data.instance_name}</p>
                                                    <p className="text-[11px] text-slate-400 truncate">
                                                        {lookup.data.instance_url || "URL not shared"}
                                                        {lookup.data.instance_version && ` · v${lookup.data.instance_version}`}
                                                    </p>
                                                </div>
                                            </div>

                                            {!pending ? (
                                                <p className="text-[12.5px] text-slate-500">This code has already been {lookup.data.status}.</p>
                                            ) : (
                                                <>
                                                    <div>
                                                        <label className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium block mb-1.5">Workspace</label>
                                                        <select
                                                            value={orgId}
                                                            onChange={(e) => setOrgId(e.target.value)}
                                                            className="w-full h-8 px-2.5 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                                        >
                                                            {(orgs.data ?? []).map((o) => (
                                                                <option key={o.id} value={o.id}>
                                                                    {o.name}
                                                                </option>
                                                            ))}
                                                        </select>
                                                    </div>
                                                    <ul className="space-y-1.5 text-[12px] text-slate-600">
                                                        <li className="flex items-start gap-2">
                                                            <ShieldCheckIcon className="w-3.5 h-3.5 mt-0.5 text-sky-600 shrink-0" />
                                                            The instance can enroll mailboxes into this workspace's warmup pool and read their warmup state.
                                                        </li>
                                                        <li className="flex items-start gap-2">
                                                            <ShieldCheckIcon className="w-3.5 h-3.5 mt-0.5 text-sky-600 shrink-0" />
                                                            It cannot see campaigns, contacts or inbox in this workspace. You can unlink it from Settings at any time.
                                                        </li>
                                                    </ul>
                                                    <div className="flex items-center gap-2 pt-1">
                                                        <button
                                                            type="button"
                                                            onClick={doDeny}
                                                            disabled={deny.isPending || approve.isPending}
                                                            className="h-9 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[13px] text-slate-700 transition-colors"
                                                        >
                                                            Decline
                                                        </button>
                                                        <button
                                                            type="button"
                                                            onClick={doApprove}
                                                            disabled={!orgId || approve.isPending || deny.isPending}
                                                            className="flex-1 h-9 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[13px] font-medium inline-flex items-center justify-center gap-1.5 transition-colors disabled:opacity-60"
                                                        >
                                                            {approve.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CheckIcon className="w-3.5 h-3.5" />}
                                                            Link instance
                                                        </button>
                                                    </div>
                                                </>
                                            )}
                                        </motion.div>
                                    )}
                                </AnimatePresence>
                            </motion.div>
                        )}
                    </AnimatePresence>
                </motion.div>
            </div>
        </div>
    );
}
