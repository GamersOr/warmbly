// Forms: hosted lead-capture forms (issue #267). Each row is one form with
// its live view/submission counters; clicking opens the builder.

import React from "react";
import { useNavigate } from "react-router-dom";
import { ClipboardListIcon, LinkIcon, MoreHorizontalIcon, PlusIcon } from "lucide-react";
import toast from "react-hot-toast";

import { EmptyBlock, Page, PageBody, PageTopbar, SectionBar, Stat, StatStrip, TopbarAction } from "@/components/layout/Page";
import { NoAccess } from "@/components/layout/NoAccess";
import { SearchInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission, useWriteGuard } from "@/hooks/usePermission";
import { useCreateForm, useDeleteForm, useForms, useUpdateForm } from "@/lib/api/hooks/app/forms";
import type Form from "@/lib/api/models/app/forms/Form";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

const STATUS_PILL: Record<Form["status"], string> = {
    draft: "bg-slate-100 text-slate-600",
    published: "bg-emerald-50 text-emerald-700",
    archived: "bg-amber-50 text-amber-700",
};

function conversion(f: Form): string {
    if (f.views_count <= 0) return "–";
    return `${Math.min(100, (f.submissions_count / f.views_count) * 100).toFixed(1)}%`;
}

function timeAgo(d?: Date): string {
    if (!d) return "never";
    const ms = Date.now() - new Date(d).getTime();
    const min = Math.floor(ms / 60_000);
    if (min < 1) return "just now";
    if (min < 60) return `${min}m ago`;
    const h = Math.floor(min / 60);
    if (h < 24) return `${h}h ago`;
    const days = Math.floor(h / 24);
    if (days < 30) return `${days}d ago`;
    return new Date(d).toLocaleDateString();
}

export default function FormsPage() {
    const canView = usePermission("VIEW_CONTACTS");
    if (!canView) return <NoAccess feature="forms" permissionLabel="View contacts" />;
    return <FormsList />;
}

function FormsList() {
    const navigate = useNavigate();
    const confirm = useConfirm();
    const write = useWriteGuard("MANAGE_CONTACTS");
    const guarded = (fn: () => void) => () => write.guard(fn)({});
    const forms = useForms();
    const create = useCreateForm();
    const update = useUpdateForm();
    const remove = useDeleteForm();

    const [query, setQuery] = React.useState("");

    const list = React.useMemo(() => {
        const all = forms.data ?? [];
        const q = query.trim().toLowerCase();
        if (!q) return all;
        return all.filter((f) => f.name.toLowerCase().includes(q));
    }, [forms.data, query]);

    const totals = React.useMemo(() => {
        const all = forms.data ?? [];
        const views = all.reduce((n, f) => n + f.views_count, 0);
        const subs = all.reduce((n, f) => n + f.submissions_count, 0);
        return {
            forms: all.length,
            live: all.filter((f) => f.status === "published").length,
            views,
            subs,
            conversion: views > 0 ? `${Math.min(100, (subs / views) * 100).toFixed(1)}%` : "–",
        };
    }, [forms.data]);

    async function createNew() {
        try {
            const f = await create.mutateAsync("Untitled form");
            navigate(`/app/forms/${f.id}`);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    async function duplicate(f: Form) {
        try {
            const copy = await create.mutateAsync(`${f.name} (copy)`.slice(0, 120));
            await update.mutateAsync({
                id: copy.id,
                w: {
                    fields: f.fields,
                    design: f.design,
                    success_message: f.success_message,
                    redirect_url: f.redirect_url,
                    campaign_id: f.campaign_id ?? null,
                    category_ids: f.category_ids,
                    allowed_domains: f.allowed_domains,
                    captcha_enabled: f.captcha_enabled,
                },
            });
            toast.success(`Duplicated as ${copy.name}`);
            navigate(`/app/forms/${copy.id}`);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    async function setStatus(f: Form, status: Form["status"]) {
        try {
            await update.mutateAsync({ id: f.id, w: { status } });
            toast.success(status === "published" ? "Form published" : status === "draft" ? "Form unpublished" : "Form archived");
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    function askDelete(f: Form) {
        confirm.show(`Delete the form "${f.name}" and its ${f.submissions_count.toLocaleString()} submissions? Contacts it created are kept.`, async () => {
            try {
                await remove.mutateAsync(f.id);
                toast.success("Form deleted");
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    }

    async function copyLink(f: Form) {
        if (!f.share_url) {
            toast.error("No public URL is configured for this instance");
            return;
        }
        await navigator.clipboard.writeText(f.share_url);
        toast.success("Link copied");
    }

    return (
        <Page>
            <PageTopbar eyebrow="Forms" subtitle="Hosted lead-capture forms you can embed anywhere">
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={guarded(createNew)}>
                    New form
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Forms" value={totals.forms} sub={`${totals.live} live`} />
                <Stat label="Views" value={totals.views.toLocaleString()} sub="all time" />
                <Stat label="Submissions" value={totals.subs.toLocaleString()} sub="all time" accent={totals.subs > 0} />
                <Stat label="Conversion" value={totals.conversion} sub="submissions / views" last />
            </StatStrip>

            <SectionBar label="All forms" count={list.length}>
                <SearchInput value={query} onChange={setQuery} placeholder="Search forms…" className="w-full sm:w-64" />
            </SectionBar>

            <PageBody>
                {forms.isPending ? (
                    <div className="p-3 space-y-1.5">
                        {[...Array(4)].map((_, i) => (
                            <div key={i} className="h-11 rounded-md bg-slate-100 animate-pulse" />
                        ))}
                    </div>
                ) : forms.isError ? (
                    <EmptyBlock title="Couldn't load forms" body="Try again in a moment." />
                ) : list.length === 0 ? (
                    <EmptyBlock
                        title={query ? "No forms match" : "No forms yet"}
                        body={
                            query
                                ? "Try a different search."
                                : "Build a form, style it to match your site, and every submission becomes a contact — filed under your categories and optionally dropped straight into a campaign."
                        }
                        cta={
                            query ? undefined : (
                                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={guarded(createNew)}>
                                    New form
                                </TopbarAction>
                            )
                        }
                    />
                ) : (
                    <div>
                        {list.map((f) => (
                            <div
                                key={f.id}
                                role="link"
                                tabIndex={0}
                                onClick={() => navigate(`/app/forms/${f.id}`)}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") navigate(`/app/forms/${f.id}`);
                                }}
                                className="group h-11 px-5 flex items-center gap-3 border-b border-slate-200/60 transition-colors hover:bg-slate-50/80 cursor-pointer"
                            >
                                <ClipboardListIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                                <div className="min-w-0 flex-1">
                                    <div className="flex items-center gap-2 min-w-0">
                                        <span className="text-[12.5px] font-medium text-slate-900 truncate">{f.name}</span>
                                        <span className={`inline-flex items-center h-4 px-1.5 rounded text-[10px] font-medium ${STATUS_PILL[f.status]}`}>
                                            {f.status}
                                        </span>
                                    </div>
                                    <div className="text-[11px] text-slate-500 truncate">
                                        Last submission {timeAgo(f.last_submission_at)}
                                    </div>
                                </div>
                                <span className="hidden md:inline font-mono text-[12px] text-slate-600 tabular-nums w-20 text-right shrink-0">
                                    {f.views_count.toLocaleString()}
                                    <span className="ml-1 text-[10px] text-slate-400 font-sans">views</span>
                                </span>
                                <span className="font-mono text-[12px] text-slate-900 tabular-nums w-24 text-right shrink-0">
                                    {f.submissions_count.toLocaleString()}
                                    <span className="ml-1 text-[10px] text-slate-400 font-sans">subs</span>
                                </span>
                                <span className="hidden sm:inline font-mono text-[12px] text-slate-600 tabular-nums w-14 text-right shrink-0">
                                    {conversion(f)}
                                </span>
                                <PopoverMenu align="end">
                                    <PopoverMenuTrigger asChild>
                                        <button
                                            type="button"
                                            onClick={(e) => e.stopPropagation()}
                                            aria-label="More"
                                            className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors shrink-0"
                                        >
                                            <MoreHorizontalIcon className="w-3.5 h-3.5" />
                                        </button>
                                    </PopoverMenuTrigger>
                                    <PopoverMenuContent minWidth={190}>
                                        <PopoverMenuItem onSelect={() => navigate(`/app/forms/${f.id}`)}>Open builder</PopoverMenuItem>
                                        <PopoverMenuItem onSelect={() => navigate(`/app/forms/${f.id}?tab=submissions`)}>
                                            View submissions
                                        </PopoverMenuItem>
                                        {f.status === "published" && (
                                            <PopoverMenuItem onSelect={() => void copyLink(f)}>
                                                <span className="inline-flex items-center gap-1.5">
                                                    <LinkIcon className="w-3 h-3" /> Copy link
                                                </span>
                                            </PopoverMenuItem>
                                        )}
                                        <PopoverMenuSeparator />
                                        {f.status !== "published" && (
                                            <PopoverMenuItem onSelect={guarded(() => void setStatus(f, "published"))}>Publish</PopoverMenuItem>
                                        )}
                                        {f.status === "published" && (
                                            <PopoverMenuItem onSelect={guarded(() => void setStatus(f, "draft"))}>Unpublish</PopoverMenuItem>
                                        )}
                                        {f.status !== "archived" && (
                                            <PopoverMenuItem onSelect={guarded(() => void setStatus(f, "archived"))}>Archive</PopoverMenuItem>
                                        )}
                                        <PopoverMenuItem onSelect={guarded(() => void duplicate(f))}>Duplicate</PopoverMenuItem>
                                        <PopoverMenuSeparator />
                                        <PopoverMenuItem onSelect={guarded(() => askDelete(f))}>Delete</PopoverMenuItem>
                                    </PopoverMenuContent>
                                </PopoverMenu>
                            </div>
                        ))}
                    </div>
                )}
            </PageBody>
        </Page>
    );
}
