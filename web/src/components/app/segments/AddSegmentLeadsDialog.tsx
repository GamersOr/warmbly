// From a campaign's Leads tab: pick a segment and enrol its current members
// as leads. The mirror image of AddSegmentToCampaignDialog.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { LayersIcon, Loader2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast";

import { SearchInput } from "@/components/ui/field";
import { useAddSegmentToCampaign, useSegments } from "@/lib/api/hooks/app/segments";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

export default function AddSegmentLeadsDialog({
    open,
    onClose,
    campaign,
}: {
    open: boolean;
    onClose: () => void;
    campaign: MiniCampaign;
}) {
    const add = useAddSegmentToCampaign();
    const segments = useSegments(open);
    const [query, setQuery] = React.useState("");
    const [picked, setPicked] = React.useState<string | null>(null);

    React.useEffect(() => {
        if (!open) {
            setQuery("");
            setPicked(null);
        }
    }, [open]);

    const list = React.useMemo(() => {
        const all = segments.data ?? [];
        const q = query.trim().toLowerCase();
        return q ? all.filter((s) => s.name.toLowerCase().includes(q)) : all;
    }, [segments.data, query]);

    const busy = add.isPending;
    const requestClose = React.useCallback(() => {
        if (!busy) onClose();
    }, [busy, onClose]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    async function submit() {
        if (!picked || busy) return;
        const seg = list.find((s) => s.id === picked);
        try {
            const res = await add.mutateAsync({ id: picked, campaignId: campaign.id });
            toast.success(
                res.added === 0
                    ? `Every member of ${seg?.name ?? "the segment"} is already a lead`
                    : `Added ${res.added.toLocaleString()} lead${res.added === 1 ? "" : "s"} from ${seg?.name ?? "the segment"}`,
            );
            onClose();
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={requestClose}
                    className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="Add leads from a segment"
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[520px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[80dvh]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <LayersIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Add leads</span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">From a segment</span>
                            <span className="hidden sm:inline-flex items-center h-5 px-1.5 rounded bg-sky-50 text-sky-700 text-[10px] font-medium max-w-[200px] truncate">
                                → {campaign.name}
                            </span>
                            <button
                                type="button"
                                onClick={requestClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>
                        <div className="px-4 py-3 border-b border-slate-100 shrink-0">
                            <SearchInput value={query} onChange={setQuery} placeholder="Search segments…" autoFocus className="w-full" />
                        </div>
                        <div className="flex-1 min-h-[200px] overflow-y-auto">
                            {segments.isPending ? (
                                <div className="p-3 space-y-1.5">
                                    {[...Array(4)].map((_, i) => (
                                        <div key={i} className="h-9 rounded-md bg-slate-100 animate-pulse" />
                                    ))}
                                </div>
                            ) : list.length === 0 ? (
                                <div className="px-5 py-10 text-center">
                                    <p className="text-[12.5px] text-slate-900 font-medium">{query ? "No segments match" : "No segments yet"}</p>
                                    <p className="text-[11.5px] text-slate-400 mt-0.5">Build one under Contacts &gt; Segments first.</p>
                                </div>
                            ) : (
                                <ul className="divide-y divide-slate-100">
                                    {list.map((s) => {
                                        const on = picked === s.id;
                                        return (
                                            <li key={s.id}>
                                                <button
                                                    type="button"
                                                    onClick={() => setPicked(s.id)}
                                                    aria-pressed={on}
                                                    className={cn(
                                                        "w-full px-4 h-10 flex items-center gap-3 text-left transition-colors",
                                                        on ? "bg-sky-50/60 hover:bg-sky-50" : "hover:bg-slate-50",
                                                    )}
                                                >
                                                    <span
                                                        className={cn(
                                                            "size-3.5 rounded-full border flex items-center justify-center shrink-0",
                                                            on ? "border-sky-600 bg-sky-600" : "border-slate-300 bg-white",
                                                        )}
                                                    >
                                                        {on && <span className="size-1.5 rounded-full bg-white" />}
                                                    </span>
                                                    <span className="size-2 rounded-full shrink-0" style={{ backgroundColor: s.color }} />
                                                    <span className="text-[12.5px] text-slate-900 font-medium truncate">{s.name}</span>
                                                    <span className="ml-auto font-mono text-[11px] text-slate-500 tabular-nums">
                                                        {s.contact_count.toLocaleString()}
                                                    </span>
                                                </button>
                                            </li>
                                        );
                                    })}
                                </ul>
                            )}
                        </div>
                        <footer className="px-3 h-12 border-t border-slate-200 flex items-center gap-2 shrink-0 bg-slate-50/30">
                            <span className="text-[11px] text-slate-400 min-w-0 truncate">Adds today's members. Existing leads are skipped.</span>
                            <button
                                type="button"
                                onClick={requestClose}
                                disabled={busy}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={busy || !picked}
                                className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <LayersIcon className="w-3 h-3" />}
                                Add leads
                            </button>
                        </footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
