import Request from "../../Request";
import type Form from "@/lib/api/models/app/forms/Form";
import type { FormsConfig, FormSubmission, FormWrite } from "@/lib/api/models/app/forms/Form";

export async function listForms(): Promise<Form[]> {
    const res = await Request<{ data: Form[] }>({ method: "GET", url: "/forms", authorization: true });
    return res.data ?? [];
}

export async function getForm(id: string): Promise<Form> {
    return await Request<Form>({ method: "GET", url: `/forms/${id}`, authorization: true });
}

export async function getFormsConfig(): Promise<FormsConfig> {
    return await Request<FormsConfig>({ method: "GET", url: "/forms/config", authorization: true });
}

export async function createForm(name: string): Promise<Form> {
    return await Request<Form>({ method: "POST", url: "/forms", data: { name }, authorization: true });
}

export async function updateForm(id: string, data: FormWrite): Promise<Form> {
    return await Request<Form>({ method: "PATCH", url: `/forms/${id}`, data, authorization: true });
}

export async function deleteForm(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/forms/${id}`, authorization: true });
}

export async function listFormSubmissions(
    id: string,
    limit = 50,
    before?: string,
): Promise<{ data: FormSubmission[]; has_more: boolean }> {
    const params = new URLSearchParams({ limit: String(limit) });
    if (before) params.set("before", before);
    const res = await Request<{ data: FormSubmission[]; has_more: boolean }>({
        method: "GET",
        url: `/forms/${id}/submissions?${params.toString()}`,
        authorization: true,
    });
    return { data: res.data ?? [], has_more: res.has_more ?? false };
}

export async function deleteFormSubmission(formId: string, submissionId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/forms/${formId}/submissions/${submissionId}`, authorization: true });
}
