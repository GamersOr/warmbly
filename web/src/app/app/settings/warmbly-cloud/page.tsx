// Settings → Warmbly Cloud.
//
// Self-hosted: link this instance to the hosted warmup pool and pick the
// mailboxes it warms (GET/POST /cloud-link/*). Cloud: the self-hosted
// instances linked to this workspace (GET /pool-link/instances).

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import { CloudIcon, ExternalLinkIcon, Loader2Icon, RefreshCwIcon } from "lucide-react";
import { usePermission } from "@/hooks/usePermission";
import { useConfirm } from "@/hooks/context/confirm";
import { NoAccess } from "@/components/layout/NoAccess";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { useCloudLinkStatus, useDisconnectCloudLink } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import { Row, Section, SectionShell } from "../_components/SectionShell";
import ConnectFlow from "./ConnectFlow";
import MailboxTable from "./MailboxTable";
import LinkedInstances from "./LinkedInstances";

export default function WarmblyCloudSettingsPage() {
    const canManage = usePermission("MANAGE_SETTINGS");
    const authConfig = useAuthConfig();
    if (!canManage) return <NoAccess feature="Warmbly Cloud" permissionLabel="Manage settings" />;
    if (authConfig.data && !authConfig.data.self_hosted) {
        return (
            <SectionShell title="Linked instances" description="Self-hosted Warmbly instances that warm their mailboxes in this workspace's pool.">
                <Section eyebrow="Instances" description="Each instance enrolls its own mailboxes. Unlinking removes them from the pool.">
                    <LinkedInstances />
                </Section>
            </SectionShell>
        );
    }
    return <SelfHostedCloud />;
}

function SelfHostedCloud() {
    const status = useCloudLinkStatus();
    const disconnect = useDisconnectCloudLink();
    const confirm = useConfirm();
    // The step flow stays up after linking until the user finishes it, so a
    // fresh link walks through mailbox selection instead of dropping into the
    // steady-state table.
    const [flow, setFlow] = React.useState<boolean | null>(null);
    const showFlow = flow ?? (status.data ? !status.data.connected : false);

    if (status.isLoading || !status.data) {
        return (
            <SectionShell title="Warmbly Cloud" description="Warm your mailboxes in the Warmbly pool while everything else stays on this server.">
                <div className="py-10 flex justify-center text-slate-400">
                    <Loader2Icon className="w-4 h-4 animate-spin" />
                </div>
            </SectionShell>
        );
    }
    const st = status.data;
    const plan = st.info?.plan;

    return (
        <SectionShell
            title="Warmbly Cloud"
            description="Warm your mailboxes in the Warmbly pool while everything else stays on this server."
            actions={
                st.connected ? (
                    <button
                        type="button"
                        onClick={() => void status.refetch()}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5"
                    >
                        <RefreshCwIcon className={`w-3 h-3 ${status.isFetching ? "animate-spin" : ""}`} />
                        Refresh
                    </button>
                ) : undefined
            }
        >
            <AnimatePresence mode="wait" initial={false}>
                {showFlow ? (
                    <motion.div key="flow" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
                        <ConnectFlow
                            status={st}
                            onFinished={() => {
                                setFlow(false);
                                void status.refetch();
                            }}
                        />
                    </motion.div>
                ) : (
                    <motion.div key="steady" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="divide-y divide-slate-200/70">
                        <Section eyebrow="Connection">
                            <Row
                                label={
                                    <span className="inline-flex items-center gap-2">
                                        <span className="size-6 rounded-md bg-sky-600 text-white inline-flex items-center justify-center">
                                            <CloudIcon className="w-3.5 h-3.5" />
                                        </span>
                                        {st.link?.organization_name || "Warmbly Cloud"}
                                    </span>
                                }
                                description={
                                    st.reachable ? (
                                        <span>
                                            Connected{st.link?.connected_at ? ` since ${new Date(st.link.connected_at).toLocaleDateString()}` : ""} ·{" "}
                                            {plan?.mailbox_limit === null || plan?.mailbox_limit === undefined
                                                ? "Unlimited mailboxes"
                                                : `${plan.enrolled} of ${plan.mailbox_limit} free mailboxes`}
                                        </span>
                                    ) : (
                                        <span className="text-amber-700">Cloud unreachable{st.error ? `: ${st.error}` : ""}</span>
                                    )
                                }
                            >
                                <div className="flex items-center gap-2">
                                    {plan && plan.tier === "free" && plan.upgrade_url && (
                                        <a
                                            href={plan.upgrade_url}
                                            target="_blank"
                                            rel="noreferrer"
                                            className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                                        >
                                            Unlimited for ${plan.price_usd}/mo
                                            <ExternalLinkIcon className="w-3 h-3" />
                                        </a>
                                    )}
                                    {plan && (
                                        <span
                                            className={`inline-flex items-center h-5 px-1.5 rounded text-[11px] font-medium ${
                                                plan.tier === "paid" ? "bg-sky-50 text-sky-700" : "bg-slate-100 text-slate-600"
                                            }`}
                                        >
                                            {plan.tier === "paid" ? "Unlimited" : "Free"}
                                        </span>
                                    )}
                                </div>
                            </Row>
                        </Section>
                        <Section
                            eyebrow="Mailboxes"
                            description="Enrolled mailboxes are warmed by Warmbly Cloud; their local warmup stops. Campaigns keep sending from this server."
                        >
                            <MailboxTable />
                        </Section>
                        <Section eyebrow="Disconnect">
                            <Row
                                danger
                                label="Disconnect from Warmbly Cloud"
                                description="Every enrolled mailbox is removed from the pool and the cloud deletes its credentials. Local warmup takes over again."
                            >
                                <button
                                    type="button"
                                    onClick={() =>
                                        confirm.show("Disconnect this instance from Warmbly Cloud? All enrolled mailboxes stop warming in the pool.", async () => {
                                            try {
                                                await disconnect.mutateAsync();
                                                setFlow(null);
                                                toast.success("Disconnected");
                                            } catch (e) {
                                                toast.error(buildError(e as AppError));
                                            }
                                        })
                                    }
                                    className="h-7 px-2.5 rounded-md text-[12px] text-rose-600 hover:bg-rose-50 transition-colors"
                                >
                                    Disconnect
                                </button>
                            </Row>
                        </Section>
                    </motion.div>
                )}
            </AnimatePresence>
        </SectionShell>
    );
}
