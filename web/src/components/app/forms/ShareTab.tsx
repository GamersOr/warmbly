// ShareTab — the hosted link and the three embed options (iframe, script,
// popup), each as a copyable snippet.

import React from "react";
import { CheckIcon, CopyIcon } from "lucide-react";

import type Form from "@/lib/api/models/app/forms/Form";

function Snippet({ label, hint, code }: { label: string; hint?: string; code: string }) {
    const [copied, setCopied] = React.useState(false);
    async function copy() {
        await navigator.clipboard.writeText(code);
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
    }
    return (
        <div>
            <div className="flex items-baseline gap-2 mb-1">
                <span className="text-[12.5px] font-medium text-slate-900">{label}</span>
                {hint && <span className="text-[11px] text-slate-500">{hint}</span>}
            </div>
            <div className="relative">
                <pre className="rounded-md border border-slate-200 bg-slate-50 p-3 pr-16 text-[11.5px] leading-relaxed text-slate-700 overflow-x-auto whitespace-pre-wrap break-all">
                    {code}
                </pre>
                <button
                    type="button"
                    onClick={() => void copy()}
                    className="absolute top-2 right-2 inline-flex items-center gap-1 h-6 px-2 rounded-md border border-slate-200 bg-white text-[11px] text-slate-600 hover:bg-slate-50"
                >
                    {copied ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <CopyIcon className="w-3 h-3" />}
                    {copied ? "Copied" : "Copy"}
                </button>
            </div>
        </div>
    );
}

export default function ShareTab({ form, baseUrl }: { form: Form; baseUrl: string }) {
    const pageUrl = form.share_url || (baseUrl ? `${baseUrl}/f/${form.public_id}` : "");
    const scriptUrl = baseUrl ? `${baseUrl}/forms.js` : "";

    if (!pageUrl) {
        return (
            <div className="p-6 text-[12.5px] text-slate-500">
                No public URL is configured for this instance. Set API_PUBLIC_URL (or FORMS_DOMAIN) on the backend.
            </div>
        );
    }

    return (
        <div className="max-w-2xl mx-auto flex flex-col gap-6 p-6">
            {form.status !== "published" && (
                <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] text-amber-800">
                    This form is not published yet. The link and embeds below go live the moment you publish.
                </div>
            )}

            <Snippet label="Hosted link" hint="share it anywhere, no website needed" code={pageUrl} />

            <Snippet
                label="JavaScript embed"
                hint="recommended: the form sizes itself to its content"
                code={`<script src="${scriptUrl}" async></script>\n<div data-warmbly-form="${form.public_id}"></div>`}
            />

            <Snippet
                label="Popup"
                hint="opens the form in an overlay when clicked"
                code={`<script src="${scriptUrl}" async></script>\n<button data-warmbly-popup="${form.public_id}">Get started</button>`}
            />

            <Snippet
                label="Plain iframe"
                hint="for builders that strip scripts; fixed height"
                code={`<iframe src="${pageUrl}?embed=1" width="100%" height="600" style="border:0" title="${form.name.replace(/"/g, "&quot;")}"></iframe>`}
            />

            <p className="text-[11.5px] text-slate-500">
                These work in WordPress, Webflow, Shopify, Framer and any site that accepts custom HTML.
                {form.allowed_domains.length > 0
                    ? ` Embedding is limited to: ${form.allowed_domains.join(", ")}.`
                    : " Restrict which sites may embed this form from the Settings tab."}
            </p>
        </div>
    );
}
