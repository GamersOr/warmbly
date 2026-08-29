import removeEmail from "@/lib/api/client/app/emails/removeEmail";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import patchEmailLists from "./patchEmailLists";

export default function useRemoveEmail(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => removeEmail(id),
        onSuccess: () => {
            patchEmailLists(queryClient, (rows) => rows.filter((c) => c.id !== id));

            queryClient.invalidateQueries({
                queryKey: ["emails", id]
            });
        }
    })
}
