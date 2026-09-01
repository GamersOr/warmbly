// SubmissionsTab — the responses table. The newest page stays live through
// the realtime spine; older pages are appended on demand.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { InboxIcon, Trash2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast";

import { EmptyBlock } from "@/components/layout/Page";
import { useConfirm } from "@/hooks/context/confirm";
import { useWriteGuard } from "@/hooks/usePermission";
import { listFormSubmissions } from "@/lib/api/client/app/forms";
import { useDeleteFormSubmission, useFormSubmissions } from "@/lib/api/hooks/app/forms";
import type Form from "@/lib/api/models/app/forms/Form";
import type { FormSubmission } from "@/lib/api/models/app/forms/Form";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

function answerText(v: string | string[] | undefined): string {
    if (v === undefined) return "";
    return Array.isArray(v) ? v.join(", ") : String(v);
}

export default function SubmissionsTab({ form }: { form: Form }) {
    const confirm = useConfirm();
    const write = useWriteGuard("MANAGE_CONTACTS");
    const first = useFormSubmissions(form.id);
    const remove = useDeleteFormSubmission();

    // Older pages, loaded past the live first page.
    const [older, setOlder] = React.useState<FormSubmission[]>([]);
    const [olderHasMore, setOlderHasMore] = React.useState(false);
    const [loadingMore, setLoadingMore] = React.useState(false);
    const [selected, setSelected] = React.useState<FormSubmission | null>(null);

    const firstPage = React.useMemo(() => first.data?.data ?? [], [first.data]);
    const firstIds = React.useMemo(() => new Set(firstPage.map((s) => s.id)), [firstPage]);
    const rows = React.useMemo(
        () => [...firstPage, ...older.filter((s) => !firstIds.has(s.id))],
        [firstPage, older, firstIds],
    );
    const hasMore = older.length > 0 ? olderHasMore : (first.data?.has_more ?? false);

    async function loadMore() {
        const last = rows[rows.length - 1];
        if (!last) return;
        setLoadingMore(true);
        try {
            const res = await listFormSubmissions(form.id, 50, new Date(last.created_at).toISOString());
            setOlder((prev) => [...prev, ...res.data]);
            setOlderHasMore(res.has_more);
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setLoadingMore(false);
        }
    }

    function askDelete(s: FormSubmission) {
        write.guard(() => {
            confirm.show("Delete this submission? The contact it created is kept.", async () => {
                try {
                    await remove.mutateAsync({ formId: form.id, submissionId: s.id });
                    setOlder((prev) => prev.filter((x) => x.id !== s.id));
                    setSelected((cur) => (cur?.id === s.id ? null : cur));
                    toast.success("Submission deleted");
                } catch (err) {
                    toast.error(buildError(err as AppError));
                }
            });
        })({});
    }

    const labelFor = React.useMemo(() => {
        const map = new Map<string, string>();
        for (const f of form.fields) map.set(f.id, f.label || f.id);
        return map;
    }, [form.fields]);

    if (first.isPending) {
        return (
            <div className="p-4 space-y-1.5">
                {[...Array(5)].map((_, i) => (
                    <div key={i} className="h-11 rounded-md bg-slate-100 animate-pulse" />
                ))}
            </div>
        );
    }

    if (rows.length === 0) {
        return (
            <EmptyBlock
                title="No submissions yet"
                body={
                    form.status === "published"
                        ? "Share the form or embed it on your site; responses land here in real time."
                        : "Publish the form to start collecting responses."
                }
            />
        );
    }

    return (
        <div className="max-w-4xl mx-auto p-4">
            <div className="rounded-md border border-slate-200 overflow-hidden">
                {rows.map((s) => (
                    <div
                        key={s.id}
                        role="link"
                        tabIndex={0}
                        onClick={() => setSelected(s)}
                        onKeyDown={(e) => {
                            if (e.key === "Enter") setSelected(s);
                        }}
                        className="group h-11 px-4 flex items-center gap-3 border-b border-slate-200/60 last:border-b-0 hover:bg-slate-50/80 cursor-pointer"
                    >
                        <InboxIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                        <div className="min-w-0 flex-1">
                            <div className="text-[12.5px] font-medium text-slate-900 truncate">
                                {s.contact_email || answerText(Object.values(s.data)[0] as string | string[]) || "Anonymous"}
                            </div>
                            {s.contact_name && <div className="text-[11px] text-slate-500 truncate">{s.contact_name}</div>}
                        </div>
                        {s.contact_id && (
                            <span className="hidden sm:inline-flex items-center h-4 px-1.5 rounded bg-emerald-50 text-emerald-700 text-[10px] font-medium shrink-0">
                                contact
                            </span>
                        )}
                        <span className="text-[11px] text-slate-400 tabular-nums shrink-0">
                            {new Date(s.created_at).toLocaleString()}
                        </span>
                        <button
                            type="button"
                            aria-label="Delete submission"
                            onClick={(e) => {
                                e.stopPropagation();
                                askDelete(s);
                            }}
                            className="size-7 rounded-md text-slate-400 hover:text-rose-600 hover:bg-rose-50 inline-flex items-center justify-center transition-colors shrink-0 opacity-100 md:opacity-0 md:group-hover:opacity-100"
                        >
                            <Trash2Icon className="w-3.5 h-3.5" />
                        </button>
                    </div>
                ))}
            </div>
            {hasMore && (
                <div className="flex justify-center mt-3">
                    <button
                        type="button"
                        disabled={loadingMore}
                        onClick={() => void loadMore()}
                        className="h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50 disabled:opacity-50"
                    >
                        {loadingMore ? "Loading…" : "Load older"}
                    </button>
                </div>
            )}

            <AnimatePresence>
                {selected && (
                    <>
                        <motion.div
                            key="backdrop"
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            exit={{ opacity: 0 }}
                            onMouseDown={() => setSelected(null)}
                            className="fixed inset-0 z-40 bg-slate-900/30 backdrop-blur-[2px]"
                        />
                        <motion.aside
                            key="panel"
                            initial={{ x: "100%" }}
                            animate={{ x: 0 }}
                            exit={{ x: "100%" }}
                            transition={{ type: "spring", damping: 32, stiffness: 320 }}
                            className="fixed right-0 top-0 bottom-0 z-50 w-[460px] max-w-[95%] bg-white shadow-xl flex flex-col"
                            onMouseDown={(e) => e.stopPropagation()}
                        >
                            <div className="shrink-0 h-12 px-4 flex items-center justify-between border-b border-slate-200">
                                <div className="min-w-0">
                                    <div className="text-[12.5px] font-medium text-slate-900 truncate">
                                        {selected.contact_email || "Submission"}
                                    </div>
                                    <div className="text-[11px] text-slate-500">{new Date(selected.created_at).toLocaleString()}</div>
                                </div>
                                <button
                                    type="button"
                                    aria-label="Close"
                                    onClick={() => setSelected(null)}
                                    className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center"
                                >
                                    <XIcon className="w-3.5 h-3.5" />
                                </button>
                            </div>
                            <div className="flex-1 overflow-y-auto p-4 flex flex-col gap-3">
                                {Object.entries(selected.data).map(([key, value]) => (
                                    <div key={key}>
                                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                            {labelFor.get(key) ?? key}
                                        </div>
                                        <div className="text-[12.5px] text-slate-900 whitespace-pre-wrap break-words">
                                            {answerText(value)}
                                        </div>
                                    </div>
                                ))}
                                {selected.source_url && (
                                    <div>
                                        <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Submitted from</div>
                                        <div className="text-[12px] text-slate-600 break-all">{selected.source_url}</div>
                                    </div>
                                )}
                            </div>
                            <div className="shrink-0 px-4 py-3 border-t border-slate-200 flex justify-end">
                                <button
                                    type="button"
                                    onClick={() => askDelete(selected)}
                                    className="h-7 px-3 rounded-md text-[12px] text-rose-600 hover:bg-rose-50 inline-flex items-center gap-1.5"
                                >
                                    <Trash2Icon className="w-3 h-3" /> Delete submission
                                </button>
                            </div>
                        </motion.aside>
                    </>
                )}
            </AnimatePresence>
        </div>
    );
}
