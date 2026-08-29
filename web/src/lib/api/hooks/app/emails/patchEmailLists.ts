import type GetEmails from "@/lib/api/models/app/emails/GetEmails";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type { InfiniteData, QueryClient } from "@tanstack/react-query";

type EmailListCache = InfiniteData<GetEmails> | Inbox[];

// Two shapes live under ["emails", "list"]: the paginated accounts list
// (InfiniteData pages) and the store's flat mailbox directory (Inbox[]).
// Patching both through one helper is what keeps a mutation's cache write
// from assuming one shape and crashing on the other.
export default function patchEmailLists(queryClient: QueryClient, patch: (rows: Inbox[]) => Inbox[]) {
    const allLists = queryClient.getQueriesData<EmailListCache>({ queryKey: ["emails", "list"] });

    for (const [key, oldData] of allLists) {
        if (!oldData) continue;

        if (Array.isArray(oldData)) {
            queryClient.setQueryData(key, patch(oldData));
            continue;
        }

        if (!Array.isArray(oldData.pages)) continue;

        queryClient.setQueryData(key, {
            ...oldData,
            pages: oldData.pages.map((page) => ({
                ...page,
                data: patch(page.data ?? []),
            })),
        });
    }
}
