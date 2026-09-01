// SettingsPanel — what happens after a submission, plus spam protection.

import React from "react";
import { PlusIcon, XIcon } from "lucide-react";

import { Label, TextInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import { SettingRow, Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";

export interface FormSettingsDraft {
    success_message: string;
    redirect_url: string;
    campaign_id: string | null;
    category_ids: string[];
    allowed_domains: string[];
    captcha_enabled: boolean;
}

export default function SettingsPanel({
    draft,
    captchaAvailable,
    onChange,
}: {
    draft: FormSettingsDraft;
    captchaAvailable: boolean;
    onChange: (patch: Partial<FormSettingsDraft>) => void;
}) {
    const campaigns = useCampaigns({ query: "", folder: "" });
    const [domainInput, setDomainInput] = React.useState("");

    function addDomain() {
        const host = domainInput
            .trim()
            .toLowerCase()
            .replace(/^https?:\/\//, "")
            .replace(/[/?#].*$/, "");
        if (!host) return;
        if (!draft.allowed_domains.includes(host)) {
            onChange({ allowed_domains: [...draft.allowed_domains, host] });
        }
        setDomainInput("");
    }

    return (
        <div className="flex flex-col gap-5 p-4">
            <section className="flex flex-col gap-3">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">After submission</span>
                <div>
                    <Label>Success message</Label>
                    <textarea
                        value={draft.success_message}
                        onChange={(e) => onChange({ success_message: e.target.value })}
                        rows={3}
                        className="w-full rounded-md border border-slate-200 px-2.5 py-1.5 text-[16px] md:text-[12.5px] text-slate-900 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                    />
                </div>
                <div>
                    <Label>Redirect URL</Label>
                    <TextInput
                        value={draft.redirect_url}
                        onChange={(v) => onChange({ redirect_url: v })}
                        placeholder="https://example.com/thanks (optional)"
                    />
                    <p className="text-[11px] text-slate-500 mt-1">Leave empty to show the success message instead.</p>
                </div>
            </section>

            <section className="flex flex-col gap-3 border-t border-slate-200 pt-4">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Lead capture</span>
                <div>
                    <Label>Add to categories</Label>
                    <CategoryPicker
                        value={draft.category_ids}
                        onChange={(next) => onChange({ category_ids: next })}
                        placeholder="Pick categories, e.g. Website leads"
                    />
                    <p className="text-[11px] text-slate-500 mt-1">Every submitted contact is filed under these.</p>
                </div>
                <div>
                    <Label>Add to campaign</Label>
                    <SelectMenu
                        value={draft.campaign_id ?? ""}
                        onChange={(v) => onChange({ campaign_id: v === "" ? null : v })}
                        options={[
                            { value: "", label: "None" },
                            ...(campaigns.campaigns ?? []).map((c) => ({ value: c.id, label: c.name })),
                        ]}
                        fullWidth
                        aria-label="Campaign"
                    />
                    <p className="text-[11px] text-slate-500 mt-1">
                        New contacts join this campaign as leads. Sending still follows the campaign's own schedule and limits.
                    </p>
                </div>
            </section>

            <section className="flex flex-col gap-3 border-t border-slate-200 pt-4">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Spam protection</span>
                <p className="text-[11px] text-slate-500 -mt-1.5">
                    Every form already has a honeypot trap, a minimum fill time and per-address rate limits.
                </p>
                {captchaAvailable ? (
                    <SettingRow title="Captcha challenge" description="Ask visitors to pass a Cloudflare Turnstile check before submitting.">
                        <Toggle value={draft.captcha_enabled} onChange={(v) => onChange({ captcha_enabled: v })} />
                    </SettingRow>
                ) : (
                    <p className="text-[11.5px] text-slate-500 rounded-md bg-slate-50 border border-slate-200 px-3 py-2">
                        A captcha challenge becomes available once the operator configures Cloudflare Turnstile
                        (TURNSTILE_SECRET and TURNSTILE_SITE_KEY).
                    </p>
                )}
                <div>
                    <Label>Allowed embed domains</Label>
                    <div className="flex items-center gap-1.5">
                        <TextInput
                            value={domainInput}
                            onChange={setDomainInput}
                            placeholder="example.com"
                            className="flex-1"
                            onKeyDown={(e) => {
                                if (e.key === "Enter") {
                                    e.preventDefault();
                                    addDomain();
                                }
                            }}
                        />
                        <button
                            type="button"
                            onClick={addDomain}
                            aria-label="Add domain"
                            className="h-7 px-2 inline-flex items-center gap-1 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50"
                        >
                            <PlusIcon className="w-3 h-3" /> Add
                        </button>
                    </div>
                    {draft.allowed_domains.length > 0 && (
                        <div className="flex flex-wrap gap-1 mt-1.5">
                            {draft.allowed_domains.map((d) => (
                                <span key={d} className="inline-flex items-center gap-1 h-5 pl-2 pr-1 rounded bg-sky-50 text-sky-700 text-[11px]">
                                    {d}
                                    <button
                                        type="button"
                                        aria-label={`Remove ${d}`}
                                        onClick={() => onChange({ allowed_domains: draft.allowed_domains.filter((x) => x !== d) })}
                                        className="size-4 inline-flex items-center justify-center rounded hover:bg-sky-100"
                                    >
                                        <XIcon className="w-2.5 h-2.5" />
                                    </button>
                                </span>
                            ))}
                        </div>
                    )}
                    <p className="text-[11px] text-slate-500 mt-1">
                        With domains listed, only those sites (and their subdomains) can embed the form. Empty allows any site.
                    </p>
                </div>
            </section>
        </div>
    );
}
