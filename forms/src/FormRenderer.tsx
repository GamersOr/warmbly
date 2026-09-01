// The live form: field state and validation via TanStack Form, submission to
// the same-origin /api. Client checks are a courtesy; the backend re-validates
// everything and its message wins the error box.

import { useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";

import type { FormField, PublicForm } from "./api";
import { submitForm, SubmitRejectedError } from "./api";
import type { ResolvedDesign } from "./design";
import { postSubmitted, redirect } from "./embed";
import type { AnswerValue } from "./fields";
import { FieldControl } from "./fields";
import { Turnstile } from "./Turnstile";
import { resetTurnstile } from "./turnstile";

type Answers = Record<string, AnswerValue>;

const LAYOUT_TYPES = new Set(["heading", "paragraph", "divider", "hidden"]);

function isInput(f: FormField): boolean {
    return !LAYOUT_TYPES.has(f.type);
}

function defaultsFor(fields: FormField[]): Answers {
    const out: Answers = {};
    for (const f of fields) {
        if (!isInput(f)) continue;
        out[f.id] = f.type === "checkboxes" ? [] : f.type === "checkbox" ? false : "";
    }
    return out;
}

function validateField(f: FormField, v: AnswerValue): string | undefined {
    const empty =
        f.type === "checkboxes" ? !Array.isArray(v) || v.length === 0 : f.type === "checkbox" ? v !== true : typeof v !== "string" || v.trim() === "";
    if (f.required && empty) return `${f.label || "This field"} is required.`;
    if (f.type === "email" && typeof v === "string" && v.trim() !== "" && !/^\S+@\S+\.\S+$/.test(v.trim())) {
        return "Enter a valid email address.";
    }
    return undefined;
}

function buildAnswers(fields: FormField[], value: Answers): Record<string, string[]> {
    const out: Record<string, string[]> = {};
    for (const f of fields) {
        if (f.type === "hidden") {
            if (f.value) out[f.id] = [f.value];
            continue;
        }
        if (!isInput(f)) continue;
        const v = value[f.id];
        if (f.type === "checkboxes") {
            if (Array.isArray(v) && v.length > 0) out[f.id] = v;
        } else if (f.type === "checkbox") {
            if (v === true) out[f.id] = ["yes"];
        } else if (typeof v === "string" && v.trim() !== "") {
            out[f.id] = [v];
        }
    }
    return out;
}

function SuccessBlock({ message }: { message: string }) {
    return (
        <div className="ok">
            <svg
                width="40"
                height="40"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
            >
                <circle cx="12" cy="12" r="10"></circle>
                <path d="m9 12 2 2 4-4"></path>
            </svg>
            <p>{message}</p>
        </div>
    );
}

export function FormRenderer({ form: def, design }: { form: PublicForm; design: ResolvedDesign }) {
    // Bots submit near-instantly after render; the backend discards those.
    const renderedAt = useRef(Math.floor(Date.now() / 1000));
    const honeypot = useRef<HTMLInputElement>(null);
    const [captchaToken, setCaptchaToken] = useState("");
    const [serverError, setServerError] = useState("");
    const [done, setDone] = useState<string | null>(null);

    const form = useForm({
        defaultValues: defaultsFor(def.fields),
        onSubmit: async ({ value }) => {
            setServerError("");
            try {
                const res = await submitForm(def.public_id, {
                    answers: buildAnswers(def.fields, value),
                    website: honeypot.current?.value ?? "",
                    _wt: renderedAt.current,
                    captcha_token: captchaToken || undefined,
                    source_url: document.referrer || window.location.href,
                });
                postSubmitted(def.public_id);
                if (res.redirect_url) {
                    redirect(res.redirect_url);
                    return;
                }
                setDone(res.message || "Thanks!");
            } catch (e) {
                setServerError(e instanceof SubmitRejectedError ? e.message : "Something went wrong. Try again.");
                resetTurnstile();
                setCaptchaToken("");
            }
        },
    });

    if (done !== null) return <SuccessBlock message={done} />;

    return (
        <form
            noValidate
            onSubmit={(e) => {
                e.preventDefault();
                e.stopPropagation();
                void form.handleSubmit();
            }}
        >
            <div className="hpwrap" aria-hidden="true">
                <input ref={honeypot} type="text" name="website" tabIndex={-1} autoComplete="off" />
            </div>
            <div className="grid">
                {serverError && <div className="err">{serverError}</div>}
                {def.fields.map((f) => {
                    switch (f.type) {
                        case "heading":
                            return (
                                <div key={f.id} className="fld">
                                    <h2 className="h">{f.label}</h2>
                                </div>
                            );
                        case "paragraph":
                            return (
                                <div key={f.id} className="fld">
                                    <p className="p">{f.value}</p>
                                </div>
                            );
                        case "divider":
                            return <hr key={f.id} className="d" />;
                        case "hidden":
                            return null;
                        default:
                            return (
                                <form.Field
                                    key={f.id}
                                    name={f.id}
                                    validators={{ onSubmit: ({ value }) => validateField(f, value) }}
                                >
                                    {(field) => (
                                        <div className={f.width === "half" ? "fld half" : "fld"}>
                                            {f.type !== "checkbox" && (
                                                <label className="l" htmlFor={`f-${f.id}`}>
                                                    {f.label}
                                                    {f.required && <span className="req"> *</span>}
                                                </label>
                                            )}
                                            <FieldControl
                                                field={f}
                                                value={field.state.value}
                                                onChange={(v) => field.handleChange(v)}
                                                onBlur={field.handleBlur}
                                            />
                                            {field.state.meta.errors.length > 0 && (
                                                <p className="fielderr">{String(field.state.meta.errors[0])}</p>
                                            )}
                                            {f.help_text && <p className="help">{f.help_text}</p>}
                                        </div>
                                    )}
                                </form.Field>
                            );
                    }
                })}
                {def.captcha_site_key && <Turnstile siteKey={def.captcha_site_key} onToken={setCaptchaToken} />}
                <div className="btnrow">
                    <form.Subscribe selector={(s) => s.isSubmitting}>
                        {(isSubmitting) => (
                            <button
                                className={design.btnFullWidth ? "submit full" : "submit"}
                                type="submit"
                                disabled={isSubmitting}
                            >
                                {design.btnLabel}
                            </button>
                        )}
                    </form.Subscribe>
                </div>
            </div>
        </form>
    );
}
