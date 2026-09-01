// DesignPanel — the builder's right rail on the Design tab. Everything here
// writes into the draft's design object; the canvas re-renders live.

import React from "react";

import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import { SettingRow, Segmented, Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import type { FormDesign } from "@/lib/api/models/app/forms/Form";

const SWATCHES = ["#0284c7", "#7c3aed", "#db2777", "#dc2626", "#ea580c", "#ca8a04", "#16a34a", "#0d9488", "#0f172a", "#475569"];
const HEX_RE = /^#[0-9a-fA-F]{6}$/;

function ColorField({
    label,
    value,
    fallback,
    onChange,
    swatches = false,
}: {
    label: string;
    value?: string;
    fallback: string;
    onChange: (v: string) => void;
    swatches?: boolean;
}) {
    const current = value || fallback;
    const [text, setText] = React.useState(current);
    React.useEffect(() => setText(current), [current]);
    return (
        <div>
            <Label>{label}</Label>
            <div className="flex items-center gap-1.5">
                <span className="size-7 rounded-md border border-slate-200 shrink-0" style={{ backgroundColor: current }} aria-hidden />
                <TextInput
                    value={text}
                    onChange={(v) => {
                        setText(v);
                        if (HEX_RE.test(v.trim())) onChange(v.trim().toLowerCase());
                    }}
                    onBlur={() => setText(current)}
                    placeholder={fallback}
                    className="w-24 font-mono"
                    invalid={!HEX_RE.test(text.trim())}
                    title="#rrggbb"
                />
            </div>
            {swatches && (
                <div className="flex flex-wrap gap-1 mt-1.5">
                    {SWATCHES.map((c) => (
                        <button
                            key={c}
                            type="button"
                            aria-label={`Use ${c}`}
                            onClick={() => onChange(c)}
                            className={`size-5 rounded-full border ${current === c ? "ring-2 ring-sky-400 ring-offset-1 border-transparent" : "border-slate-200"}`}
                            style={{ backgroundColor: c }}
                        />
                    ))}
                </div>
            )}
        </div>
    );
}

export default function DesignPanel({
    design,
    onChange,
}: {
    design: FormDesign;
    onChange: (patch: Partial<FormDesign>) => void;
}) {
    return (
        <div className="flex flex-col gap-5 p-4">
            <section className="flex flex-col gap-3">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Layout</span>
                <div>
                    <Label>Font</Label>
                    <SelectMenu
                        value={design.font_family ?? "system"}
                        onChange={(v) => onChange({ font_family: v })}
                        options={[
                            { value: "system", label: "System" },
                            { value: "inter", label: "Inter" },
                            { value: "serif", label: "Serif" },
                            { value: "mono", label: "Monospace" },
                        ]}
                        fullWidth
                        aria-label="Font"
                    />
                </div>
                <div className="flex items-end gap-3">
                    <div>
                        <Label>Form width</Label>
                        <NumberInput value={design.max_width ?? 560} onChange={(v) => onChange({ max_width: v })} min={320} max={960} step={20} suffix="px" className="w-28" />
                    </div>
                    <div>
                        <Label>Corner radius</Label>
                        <NumberInput value={design.border_radius ?? 10} onChange={(v) => onChange({ border_radius: v })} min={0} max={24} suffix="px" className="w-24" />
                    </div>
                </div>
                <div>
                    <Label>Field spacing</Label>
                    <Segmented
                        value={(design.spacing as "compact" | "normal" | "relaxed") || "normal"}
                        onChange={(v) => onChange({ spacing: v })}
                        options={[
                            { value: "compact", label: "Compact" },
                            { value: "normal", label: "Normal" },
                            { value: "relaxed", label: "Relaxed" },
                        ]}
                    />
                </div>
                <SettingRow title="Card shadow" description="A soft drop shadow behind the form card.">
                    <Toggle value={design.shadow ?? true} onChange={(v) => onChange({ shadow: v })} />
                </SettingRow>
            </section>

            <section className="flex flex-col gap-3 border-t border-slate-200 pt-4">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Colors</span>
                <ColorField label="Accent" value={design.accent_color} fallback="#0284c7" onChange={(v) => onChange({ accent_color: v })} swatches />
                <div className="grid grid-cols-2 gap-3">
                    <ColorField label="Page background" value={design.page_background} fallback="#f8fafc" onChange={(v) => onChange({ page_background: v })} />
                    <ColorField label="Form background" value={design.form_background} fallback="#ffffff" onChange={(v) => onChange({ form_background: v })} />
                    <ColorField label="Text" value={design.text_color} fallback="#0f172a" onChange={(v) => onChange({ text_color: v })} />
                    <ColorField label="Labels" value={design.label_color} fallback="#334155" onChange={(v) => onChange({ label_color: v })} />
                    <ColorField label="Input background" value={design.input_background} fallback="#ffffff" onChange={(v) => onChange({ input_background: v })} />
                    <ColorField label="Input border" value={design.input_border_color} fallback="#e2e8f0" onChange={(v) => onChange({ input_border_color: v })} />
                    <ColorField label="Input text" value={design.input_text_color} fallback="#0f172a" onChange={(v) => onChange({ input_text_color: v })} />
                    <ColorField label="Placeholder" value={design.placeholder_color} fallback="#94a3b8" onChange={(v) => onChange({ placeholder_color: v })} />
                </div>
            </section>

            <section className="flex flex-col gap-3 border-t border-slate-200 pt-4">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Button</span>
                <div>
                    <Label>Button text</Label>
                    <TextInput value={design.button_text ?? ""} onChange={(v) => onChange({ button_text: v })} placeholder="Submit" />
                </div>
                <div className="grid grid-cols-2 gap-3">
                    <ColorField label="Background" value={design.button_background} fallback="#0284c7" onChange={(v) => onChange({ button_background: v })} />
                    <ColorField label="Text color" value={design.button_text_color} fallback="#ffffff" onChange={(v) => onChange({ button_text_color: v })} />
                </div>
                <div>
                    <Label>Size</Label>
                    <Segmented
                        value={(design.button_size as "sm" | "md" | "lg") || "md"}
                        onChange={(v) => onChange({ button_size: v })}
                        options={[
                            { value: "sm", label: "Small" },
                            { value: "md", label: "Medium" },
                            { value: "lg", label: "Large" },
                        ]}
                    />
                </div>
                <SettingRow title="Full width" description="Stretch the button across the form.">
                    <Toggle value={design.button_full_width ?? false} onChange={(v) => onChange({ button_full_width: v })} />
                </SettingRow>
            </section>
        </div>
    );
}
