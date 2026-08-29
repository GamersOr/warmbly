import updateEmail from "@/lib/api/client/app/emails/updateEmail";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import patchEmailLists from "./patchEmailLists";

export default function useUpdateEmail(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (inbox: Partial<Inbox>) => updateEmail(id, inbox),
        onSuccess: (data) => {
            patchEmailLists(queryClient, (rows) => rows.map((c) => (c.id === id ? data : c)));

            queryClient.setQueryData<Inbox>(
                ["emails", id],
                data
            );
        }
    })
}
