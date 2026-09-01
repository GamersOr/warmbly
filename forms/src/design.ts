// Mirrors resolveDesign in web/src/components/app/forms/FormPreview.tsx (the
// builder canvas): the hosted page must render exactly what the canvas shows.
// Defaults live here so stored designs stay sparse and old forms pick up new
// defaults.

import type { FormDesign } from "./api";

export interface ResolvedDesign {
    font: string;
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

export function resolveDesign(d: FormDesign): ResolvedDesign {
    const fontStacks: Record<string, string> = {
        inter: "'Inter',system-ui,-apple-system,'Segoe UI',sans-serif",
        serif: "Georgia,'Times New Roman',serif",
        mono: "ui-monospace,SFMono-Regular,Menlo,monospace",
        system: "system-ui,-apple-system,'Segoe UI',Roboto,sans-serif",
    };
    const font = d.font_family && fontStacks[d.font_family] ? d.font_family : "system";
    const radius = d.border_radius ?? 10;
    const btnSizes: Record<string, [string, number]> = {
        sm: ["8px 16px", 13],
        md: ["10px 20px", 14],
        lg: ["13px 26px", 16],
    };
    const [btnPad, btnFont] = btnSizes[d.button_size ?? "md"] ?? btnSizes.md;
    return {
        font,
        fontStack: fontStacks[font],
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

// applyDesign paints the resolved theme onto the document as CSS custom
// properties; styles.css reads nothing but these variables.
export function applyDesign(r: ResolvedDesign) {
    const s = document.documentElement.style;
    s.setProperty("--wf-font", r.fontStack);
    s.setProperty("--wf-page-bg", r.pageBg);
    s.setProperty("--wf-form-bg", r.formBg);
    s.setProperty("--wf-text", r.text);
    s.setProperty("--wf-label", r.label);
    s.setProperty("--wf-input-bg", r.inputBg);
    s.setProperty("--wf-input-border", r.inputBorder);
    s.setProperty("--wf-input-text", r.inputText);
    s.setProperty("--wf-placeholder", r.placeholder);
    s.setProperty("--wf-accent", r.accent);
    s.setProperty("--wf-btn-bg", r.btnBg);
    s.setProperty("--wf-btn-text", r.btnText);
    s.setProperty("--wf-btn-pad", r.btnPad);
    s.setProperty("--wf-btn-font", `${r.btnFont}px`);
    s.setProperty("--wf-radius", `${r.radius}px`);
    s.setProperty("--wf-input-radius", `${r.inputRadius}px`);
    s.setProperty("--wf-max-width", `${r.maxWidth}px`);
    s.setProperty("--wf-gap", `${r.gap}px`);
    s.setProperty("--wf-shadow", r.shadow ? "0 1px 2px rgba(15,23,42,.06),0 8px 24px rgba(15,23,42,.08)" : "none");

    if (r.font === "inter" && !document.getElementById("wf-font")) {
        const link = document.createElement("link");
        link.id = "wf-font";
        link.rel = "stylesheet";
        link.href = "https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&display=swap";
        document.head.appendChild(link);
    }
}
