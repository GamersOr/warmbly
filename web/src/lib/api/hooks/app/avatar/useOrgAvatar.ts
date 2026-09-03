import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
    deleteOrganizationAvatar,
    uploadOrganizationAvatar,
} from "@/lib/api/client/app/avatar/uploadUserAvatar";
import type Organization from "@/lib/api/models/app/organizations/Organization";
import { useAppStore } from "@/stores";

// The workspace avatar is read from the persisted zustand org pointer, which
// is fed by the /organization list. Patch the store and the cached queries
// with the server's answer right away, then invalidate so the list refetch
// (and OrgGate's setOrganizations) settles on the same value.
function applyOrgAvatar(qc: QueryClient, avatarUrl: string | null) {
    const store = useAppStore.getState();
    const current = store.currentOrganization;
    if (current) {
        // setOrganizations adopts the matching row as the current org, but it
        // nulls the pointer when the list has no match, so patch the pointer
        // directly whenever the list is not loaded yet.
        const list = store.organizations;
        if (list.some((o) => o.id === current.id)) {
            store.setOrganizations(list.map((o) => (o.id === current.id ? { ...o, avatar_url: avatarUrl } : o)));
        } else {
            store.setCurrentOrganization({ ...current, avatar_url: avatarUrl });
        }
    }
    qc.setQueryData<Organization[]>(["organizations", "list"], (old) =>
        old && current ? old.map((o) => (o.id === current.id ? { ...o, avatar_url: avatarUrl } : o)) : old,
    );
    qc.setQueryData<Organization>(["organizations", "current"], (old) =>
        old ? { ...old, avatar_url: avatarUrl } : old,
    );
    qc.invalidateQueries({ queryKey: ["organizations"] });
}

export function useUploadOrgAvatar() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (blob: Blob) => uploadOrganizationAvatar(blob),
        onSuccess: (res) => applyOrgAvatar(qc, res.avatar_url),
    });
}

export function useDeleteOrgAvatar() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => deleteOrganizationAvatar(),
        onSuccess: () => applyOrgAvatar(qc, null),
    });
}
