// Mailboxes page strip for self-hosted instances: invites an unlinked
// instance to connect, and shows a linked one how many mailboxes are in the
// pool. Dismissal of the invite is remembered locally.

import React from "react";
import { Link } from "react-router-dom";
import { CloudIcon, XIcon } from "lucide-react";
import useCloudPool from "@/hooks/useCloudPool";

const DISMISS_KEY = "warmbly.cloud-pool-banner.dismissed";

export default function CloudPoolBanner({ onConnect, mailboxCount }: { onConnect: () => void; mailboxCount: number }) {
    const pool = useCloudPool();
    const [dismissed, setDismissed] = React.useState(() => localStorage.getItem(DISMISS_KEY) === "1");

    if (!pool.selfHosted || pool.loading) return null;

    if (pool.connected) {
        const limit = pool.plan?.mailbox_limit ?? null;
        return (
            <div className="px-5 pt-4">
                <div className="flex items-center gap-2.5 rounded-md border border-sky-200/70 bg-sky-50/60 px-3 py-2 text-[12.5px] text-sky-900">
                    <CloudIcon className="w-4 h-4 shrink-0 text-sky-600" />
                    <span className="min-w-0 flex-1 leading-snug">
                        <span className="font-medium">
                            {pool.enrolledCount === 0 ? "No mailbox is in the Warmbly pool yet." : `${pool.enrolledCount} of ${mailboxCount} mailboxes warm in the Warmbly pool.`}
                        </span>{" "}
                        <span className="text-sky-900/80">
                            {limit === null ? "Unlimited mailboxes." : `${pool.enrolledCount} of ${limit} free.`} Use the cloud icon on a row, or the warmup menu, to add one.
                        </span>
                    </span>
                    <Link to="/app/settings/warmbly-cloud" className="shrink-0 text-[12px] font-medium text-sky-700 hover:text-sky-900 underline underline-offset-2">
                        Manage
                    </Link>
                </div>
            </div>
        );
    }

    if (dismissed) return null;

    return (
        <div className="px-5 pt-4">
            <div className="flex items-center gap-2.5 rounded-md border border-sky-200/70 bg-gradient-to-r from-sky-50 to-white px-3 py-2.5 text-[12.5px] text-slate-800">
                <span className="size-7 rounded-md bg-sky-600 text-white inline-flex items-center justify-center shrink-0">
                    <CloudIcon className="w-3.5 h-3.5" />
                </span>
                <span className="min-w-0 flex-1 leading-snug">
                    <span className="font-medium">Warm these mailboxes in the Warmbly pool.</span>{" "}
                    <span className="text-slate-600">Thousands of real mailboxes, replies and spam rescue handled for you. Free for up to 10 mailboxes; everything else stays on this server.</span>
                </span>
                <button
                    type="button"
                    onClick={onConnect}
                    className="shrink-0 h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    Connect
                </button>
                <button
                    type="button"
                    aria-label="Dismiss"
                    onClick={() => {
                        localStorage.setItem(DISMISS_KEY, "1");
                        setDismissed(true);
                    }}
                    className="shrink-0 size-6 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-100"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>
        </div>
    );
}
