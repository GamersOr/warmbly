// FormPreview — the builder's live canvas. Renders the draft exactly the way
// the hosted page does (same defaults as resolveFormDesign in the backend),
// with selection, drag-reorder (dnd-kit) and quick actions layered on top.

import React from "react";
import { CopyIcon, GripVerticalIcon, Trash2Icon } from "lucide-react";
import { SortableContext, rectSortingStrategy, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import type { FormDesign, FormField } from "@/lib/api/models/app/forms/Form";

export interface ResolvedDesign {
    fontStack: string;
    pageBg: string;
    formBg: string;
    text: string;
    label: string;
    inputBg: string;
    inputBorder: string;
    inputText: string;
    placeholder: string;
    accent: string;
    btnBg: string;
    btnText: string;
    btnLabel: string;
    btnPad: string;
    btnFont: number;
    btnFullWidth: boolean;
    radius: number;
    inputRadius: number;
    maxWidth: number;
    gap: number;
    shadow: boolean;
}

// Mirrors resolveFormDesign in internal/api/handler/form_public.go: the
// canvas must show exactly what the hosted page will.
export function resolveDesign(d: FormDesign): ResolvedDesign {
    const fontStacks: Record<string, string> = {
        inter: "'Inter',system-ui,-apple-system,'Segoe UI',sans-serif",
        serif: "Georgia,'Times New Roman',serif",
        mono: "ui-monospace,SFMono-Regular,Menlo,monospace",
        system: "system-ui,-apple-system,'Segoe UI',Roboto,sans-serif",
    };
    const radius = d.border_radius ?? 10;
    const btnSizes: Record<string, [string, number]> = {
        sm: ["8px 16px", 13],
        md: ["10px 20px", 14],
        lg: ["13px 26px", 16],
    };
    const [btnPad, btnFont] = btnSizes[d.button_size ?? "md"] ?? btnSizes.md;
    return {
        fontStack: fontStacks[d.font_family ?? "system"] ?? fontStacks.system,
        pageBg: d.page_background || "#f8fafc",
        formBg: d.form_background || "#ffffff",
        text: d.text_color || "#0f172a",
        label: d.label_color || "#334155",
        inputBg: d.input_background || "#ffffff",
        inputBorder: d.input_border_color || "#e2e8f0",
        inputText: d.input_text_color || "#0f172a",
        placeholder: d.placeholder_color || "#94a3b8",
        accent: d.accent_color || "#0284c7",
        btnBg: d.button_background || "#0284c7",
        btnText: d.button_text_color || "#ffffff",
        btnLabel: d.button_text || "Submit",
        btnPad,
        btnFont,
        btnFullWidth: !!d.button_full_width,
        radius,
        inputRadius: Math.min(radius, 12),
        maxWidth: d.max_width ?? 560,
        gap: d.spacing === "compact" ? 10 : d.spacing === "relaxed" ? 22 : 16,
        shadow: d.shadow ?? true,
    };
}

function inputStyle(r: ResolvedDesign): React.CSSProperties {
    return {
        width: "100%",
        fontSize: 14,
        fontFamily: "inherit",
        color: r.inputText,
        background: r.inputBg,
        border: `1px solid ${r.inputBorder}`,
        borderRadius: r.inputRadius,
        padding: "9px 11px",
    };
}

// FieldBody renders one block the way the hosted page does. Inputs are
// display-only in the canvas (pointer-events off), so a click selects.
function FieldBody({ field, r }: { field: FormField; r: ResolvedDesign }) {
    const label = (
        <span style={{ display: "block", fontSize: 13, fontWeight: 500, color: r.label, marginBottom: 6 }}>
            {field.label || <em style={{ color: r.placeholder }}>Untitled</em>}
            {field.required && <span style={{ color: "#dc2626" }}> *</span>}
        </span>
    );
    const help = field.help_text ? (
        <p style={{ fontSize: 12, color: r.placeholder, margin: "5px 0 0" }}>{field.help_text}</p>
    ) : null;
    const opts = field.options ?? [];

    switch (field.type) {
        case "heading":
            return <h2 style={{ margin: "6px 0 0", fontSize: 19, fontWeight: 600, color: r.text }}>{field.label || "Heading"}</h2>;
        case "paragraph":
            return (
                <p style={{ margin: 0, fontSize: 14, lineHeight: 1.55, color: r.text, opacity: 0.85, whiteSpace: "pre-line" }}>
                    {field.value || "Text block"}
                </p>
            );
        case "divider":
            return <hr style={{ border: "none", borderTop: `1px solid ${r.inputBorder}`, margin: "6px 0" }} />;
        case "hidden":
            return (
                <div
                    style={{ fontSize: 11.5, color: r.placeholder, border: `1px dashed ${r.inputBorder}`, borderRadius: r.inputRadius, padding: "6px 10px" }}
                >
                    Hidden field · {field.id}
                    {field.value ? ` = ${field.value}` : ""} (not shown to visitors)
                </div>
            );
        case "textarea":
            return (
                <div>
                    {label}
                    <div style={{ ...inputStyle(r), minHeight: 24 * (field.rows || 4), color: r.placeholder, fontSize: 14 }}>
                        {field.placeholder || ""}
                    </div>
                    {help}
                </div>
            );
        case "select":
            return (
                <div>
                    {label}
                    <div style={{ ...inputStyle(r), color: r.placeholder, display: "flex", justifyContent: "space-between" }}>
                        <span>{field.placeholder || "Select…"}</span>
                        <span>▾</span>
                    </div>
                    {help}
                </div>
            );
        case "radio":
        case "checkboxes":
            return (
                <div>
                    {label}
                    <div style={{ display: "flex", flexDirection: "column", gap: 7 }}>
                        {opts.map((o, i) => (
                            <span key={i} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 14, color: r.text }}>
                                <span
                                    style={{
                                        width: 15,
                                        height: 15,
                                        border: `1.5px solid ${r.inputBorder}`,
                                        borderRadius: field.type === "radio" ? 8 : 4,
                                        background: r.inputBg,
                                        flexShrink: 0,
                                    }}
                                />
                                {o}
                            </span>
                        ))}
                    </div>
                    {help}
                </div>
            );
        case "checkbox":
            return (
                <div>
                    <span style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 14, color: r.text }}>
                        <span style={{ width: 15, height: 15, border: `1.5px solid ${r.inputBorder}`, borderRadius: 4, background: r.inputBg, flexShrink: 0 }} />
                        <span>
                            {field.placeholder || field.label || "I agree"}
                            {field.required && <span style={{ color: "#dc2626" }}> *</span>}
                        </span>
                    </span>
                    {help}
                </div>
            );
        default:
            return (
                <div>
                    {label}
                    <div style={{ ...inputStyle(r), color: r.placeholder }}>{field.placeholder || " "}</div>
                    {help}
                </div>
            );
    }
}

function SortableField({
    field,
    r,
    selected,
    editable,
    onSelect,
    onDelete,
    onDuplicate,
}: {
    field: FormField;
    r: ResolvedDesign;
    selected: boolean;
    editable: boolean;
    onSelect: (id: string) => void;
    onDelete: (id: string) => void;
    onDuplicate: (id: string) => void;
}) {
    const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
        id: field.id,
        disabled: !editable,
    });
    const half = field.width === "half";
    return (
        <div
            ref={setNodeRef}
            style={{
                transform: CSS.Transform.toString(transform),
                transition,
                flex: half ? `1 1 calc(50% - ${r.gap}px)` : "1 1 100%",
                minWidth: 0,
                opacity: isDragging ? 0.5 : 1,
            }}
            onClick={(e) => {
                e.stopPropagation();
                onSelect(field.id);
            }}
            className={`relative rounded-md cursor-pointer group/field ${
                selected ? "ring-2 ring-sky-400 ring-offset-2" : "hover:ring-1 hover:ring-sky-200 hover:ring-offset-2"
            }`}
            data-field-id={field.id}
        >
            <div style={{ pointerEvents: "none" }}>
                <FieldBody field={field} r={r} />
            </div>
            {editable && (
                <div
                    className={`absolute -top-2.5 right-1 z-10 flex items-center gap-0.5 rounded-md border border-slate-200 bg-white shadow-sm px-0.5 ${
                        selected ? "opacity-100" : "opacity-100 md:opacity-0 md:group-hover/field:opacity-100"
                    } transition-opacity`}
                    onClick={(e) => e.stopPropagation()}
                >
                    <button
                        type="button"
                        aria-label="Drag to reorder"
                        className="size-5 inline-flex items-center justify-center text-slate-400 hover:text-slate-700 cursor-grab active:cursor-grabbing"
                        {...attributes}
                        {...listeners}
                    >
                        <GripVerticalIcon className="w-3 h-3" />
                    </button>
                    <button
                        type="button"
                        aria-label="Duplicate field"
                        onClick={() => onDuplicate(field.id)}
                        className="size-5 inline-flex items-center justify-center text-slate-400 hover:text-slate-700"
                    >
                        <CopyIcon className="w-3 h-3" />
                    </button>
                    <button
                        type="button"
                        aria-label="Delete field"
                        onClick={() => onDelete(field.id)}
                        className="size-5 inline-flex items-center justify-center text-slate-400 hover:text-rose-600"
                    >
                        <Trash2Icon className="w-3 h-3" />
                    </button>
                </div>
            )}
        </div>
    );
}

export default function FormPreview({
    fields,
    design,
    selectedId,
    editable,
    captchaBadge,
    onSelect,
    onDelete,
    onDuplicate,
}: {
    fields: FormField[];
    design: FormDesign;
    selectedId: string | null;
    /** false renders a plain preview with no selection/drag affordances. */
    editable: boolean;
    /** Show the "protected by captcha" placeholder above the button. */
    captchaBadge?: boolean;
    onSelect: (id: string) => void;
    onDelete: (id: string) => void;
    onDuplicate: (id: string) => void;
}) {
    const r = resolveDesign(design);
    return (
        <div
            className="min-h-full w-full overflow-y-auto"
            style={{ background: r.pageBg, fontFamily: r.fontStack }}
            onClick={() => onSelect("")}
        >
            <div style={{ maxWidth: r.maxWidth, margin: "0 auto", padding: "32px 16px" }}>
                <div
                    style={{
                        background: r.formBg,
                        borderRadius: r.radius,
                        padding: 28,
                        boxShadow: r.shadow ? "0 1px 2px rgba(15,23,42,.06),0 8px 24px rgba(15,23,42,.08)" : "none",
                    }}
                >
                    <SortableContext items={fields.map((f) => f.id)} strategy={rectSortingStrategy}>
                        <div style={{ display: "flex", flexWrap: "wrap", gap: r.gap }}>
                            {fields.length === 0 && (
                                <div className="w-full rounded-md border-2 border-dashed border-slate-200 p-8 text-center text-[12.5px] text-slate-400">
                                    Add fields from the left panel
                                </div>
                            )}
                            {fields.map((f) => (
                                <SortableField
                                    key={f.id}
                                    field={f}
                                    r={r}
                                    selected={selectedId === f.id}
                                    editable={editable}
                                    onSelect={onSelect}
                                    onDelete={onDelete}
                                    onDuplicate={onDuplicate}
                                />
                            ))}
                            {captchaBadge && (
                                <div
                                    style={{
                                        width: "100%",
                                        border: `1px dashed ${r.inputBorder}`,
                                        borderRadius: r.inputRadius,
                                        padding: "10px 12px",
                                        fontSize: 12,
                                        color: r.placeholder,
                                    }}
                                >
                                    Spam protection challenge appears here
                                </div>
                            )}
                            <div style={{ width: "100%", marginTop: 4 }}>
                                <span
                                    style={{
                                        display: r.btnFullWidth ? "block" : "inline-block",
                                        textAlign: "center",
                                        fontWeight: 600,
                                        fontSize: r.btnFont,
                                        color: r.btnText,
                                        background: r.btnBg,
                                        borderRadius: r.radius,
                                        padding: r.btnPad,
                                    }}
                                >
                                    {r.btnLabel}
                                </span>
                            </div>
                        </div>
                    </SortableContext>
                </div>
                <div style={{ textAlign: "center", marginTop: 14 }}>
                    <span style={{ fontSize: 11, color: r.placeholder }}>Powered by Warmbly</span>
                </div>
            </div>
        </div>
    );
}
