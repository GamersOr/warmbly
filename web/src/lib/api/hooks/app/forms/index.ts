import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    createForm,
    deleteForm,
    deleteFormSubmission,
    getForm,
    getFormsConfig,
    listForms,
    listFormSubmissions,
    updateForm,
} from "@/lib/api/client/app/forms";
import type { FormWrite } from "@/lib/api/models/app/forms/Form";

// Every form read lives under ["forms"]: the realtime spine invalidates that
// prefix on any form mutation, and FORM_SUBMISSION_CREATED events refresh
// counters and submission lists live.
export function useForms(enabled = true) {
    return useQuery({ queryKey: ["forms", "list"], queryFn: listForms, enabled });
}

export function useForm(id: string | undefined) {
    return useQuery({ queryKey: ["forms", id], queryFn: () => getForm(id as string), enabled: !!id });
}

export function useFormsConfig() {
    return useQuery({ queryKey: ["forms", "instance-config"], queryFn: getFormsConfig, staleTime: 5 * 60 * 1000 });
}

export function useFormSubmissions(id: string | undefined, before?: string) {
    return useQuery({
        queryKey: ["forms", id, "submissions", before ?? ""],
        queryFn: () => listFormSubmissions(id as string, 50, before),
        enabled: !!id,
    });
}

function invalidateForms(queryClient: ReturnType<typeof useQueryClient>) {
    return queryClient.invalidateQueries({ queryKey: ["forms"] });
}

export function useCreateForm() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (name: string) => createForm(name),
        onSuccess: () => invalidateForms(queryClient),
    });
}

// Deliberately does NOT invalidate ["forms", id]: the builder holds the
// draft, and a refetch mid-edit would reseed an open canvas (same reasoning
// as useUpdateAutomation). The list still refreshes via the audit spine.
export function useUpdateForm() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, w }: { id: string; w: FormWrite }) => updateForm(id, w),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ["forms", "list"] }),
    });
}

export function useDeleteForm() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteForm(id),
        onSuccess: () => invalidateForms(queryClient),
    });
}

export function useDeleteFormSubmission() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ formId, submissionId }: { formId: string; submissionId: string }) =>
            deleteFormSubmission(formId, submissionId),
        onSuccess: () => invalidateForms(queryClient),
    });
}
