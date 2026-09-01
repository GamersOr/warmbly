// The same-origin JSON API the Go forms service (cmd/forms) exposes for this
// app. Mirrors internal/formserver: keep the shapes in sync with its
// public handlers.

export type FormFieldType =
    | "text"
    | "email"
    | "phone"
    | "textarea"
    | "number"
    | "select"
    | "radio"
    | "checkboxes"
    | "checkbox"
    | "date"
    | "hidden"
    | "heading"
    | "paragraph"
    | "divider";

export interface FormField {
    id: string;
    type: FormFieldType;
    label: string;
    placeholder?: string;
    help_text?: string;
    required: boolean;
    options?: string[];
    /** Hidden-field constant or paragraph body. */
    value?: string;
    /** "full" (default) or "half"; two half fields share a row. */
    width?: string;
    rows?: number;
}

export interface FormDesign {
    font_family?: string;
    page_background?: string;
    form_background?: string;
    text_color?: string;
    label_color?: string;
    input_background?: string;
    input_border_color?: string;
    input_text_color?: string;
    placeholder_color?: string;
    accent_color?: string;
    button_background?: string;
    button_text_color?: string;
    button_text?: string;
    button_size?: string;
    button_full_width?: boolean;
    border_radius?: number;
    max_width?: number;
    spacing?: string;
    shadow?: boolean;
}

export interface PublicForm {
    public_id: string;
    name: string;
    fields: FormField[];
    design: FormDesign;
    captcha_site_key?: string;
}

export interface SubmitPayload {
    answers: Record<string, string[]>;
    /** Honeypot value; a human never fills it. */
    website: string;
    /** Unix second the page was rendered; bots submit near-instantly. */
    _wt: number;
    captcha_token?: string;
    source_url?: string;
}

export interface SubmitResult {
    message: string;
    redirect_url?: string;
}

export class FormNotFoundError extends Error {
    constructor() {
        super("form not found");
        this.name = "FormNotFoundError";
    }
}

export class SubmitRejectedError extends Error {}

export async function fetchForm(publicId: string): Promise<PublicForm> {
    const res = await fetch(`/api/forms/${encodeURIComponent(publicId)}`, {
        headers: { Accept: "application/json" },
    });
    if (res.status === 404) throw new FormNotFoundError();
    if (!res.ok) throw new Error(`form fetch failed (${res.status})`);
    return (await res.json()) as PublicForm;
}

export async function submitForm(publicId: string, payload: SubmitPayload): Promise<SubmitResult> {
    const res = await fetch(`/api/forms/${encodeURIComponent(publicId)}/submit`, {
        method: "POST",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify(payload),
    });
    if (res.status === 404) throw new FormNotFoundError();
    const body = (await res.json().catch(() => null)) as { message?: string } | null;
    if (!res.ok) {
        throw new SubmitRejectedError(body?.message || "Something went wrong. Try again.");
    }
    return (body ?? { message: "Thanks!" }) as SubmitResult;
}
