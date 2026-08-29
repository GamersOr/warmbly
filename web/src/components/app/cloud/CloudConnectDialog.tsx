// Modal wrapper around CloudLinkCard for surfaces outside Settings (the
// mailboxes page). Same overlay anatomy as NewCampaignDialog.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { XIcon } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import CloudLinkCard from "./CloudLinkCard";
import { CLOUD_LINK_KEY } from "@/lib/api/hooks/app/cloudlink/useCloudLink";

export default function CloudConnectDialog({ open, onClose, cloudUrl }: { open: boolean; onClose: () => void; cloudUrl?: string }) {
    const qc = useQueryClient();
    const [linked, setLinked] = React.useState(false);
    const [orgName, setOrgName] = React.useState("");

    React.useEffect(() => {
        if (!open) {
            setLinked(false);
            setOrgName("");
        }
    }, [open]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, onClose]);

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={onClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="Connect to Warmbly Cloud"
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[520px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18)] overflow-hidden"
                    >
                        <div className="h-11 px-4 flex items-center justify-between border-b border-slate-200/70">
                            <span className="text-[12.5px] font-semibold text-slate-900">Warmbly Cloud</span>
                            <button type="button" onClick={onClose} aria-label="Close" className="size-7 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-100">
                                <XIcon className="w-4 h-4" />
                            </button>
                        </div>
                        <div className="px-5 py-5">
                            <CloudLinkCard
                                compact
                                linked={linked}
                                orgName={orgName}
                                cloudUrl={cloudUrl}
                                onLinked={(name) => {
                                    setOrgName(name);
                                    setLinked(true);
                                    void qc.invalidateQueries({ queryKey: CLOUD_LINK_KEY });
                                }}
                            />
                        </div>
                        {linked && (
                            <div className="px-5 py-3 border-t border-slate-200/70 flex justify-end">
                                <button type="button" onClick={onClose} className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors">
                                    Pick mailboxes
                                </button>
                            </div>
                        )}
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
