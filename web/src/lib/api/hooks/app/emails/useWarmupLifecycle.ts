import warmupLifecycle, { type WarmupAction } from "@/lib/api/client/app/emails/warmupLifecycle";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import patchEmailLists from "./patchEmailLists";

// Drives the flame-icon dropdown + the warmup tab's enable/pause/resume
// control. Patches the mailbox into every emails list page and the single
// mailbox cache (mirrors useUpdateEmail) and invalidates the account-status
// query so the live warmup/health panel refreshes.
export default function useWarmupLifecycle(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (action: WarmupAction) => warmupLifecycle(id, action),
        onSuccess: (data) => {
            patchEmailLists(queryClient, (rows) => rows.map((c) => (c.id === id ? data : c)));

            queryClient.setQueryData<Inbox>(["emails", id], data);
            void queryClient.invalidateQueries({ queryKey: ["analytics", "accounts", id] });
        },
    });
}
