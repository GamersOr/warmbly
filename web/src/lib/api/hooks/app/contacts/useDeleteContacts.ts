import deleteContacts from "@/lib/api/client/app/contacts/deleteContacts";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useDeleteContacts() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (contact_ids: string[]) => deleteContacts(contact_ids),
        onSuccess: (_, contact_ids) => {
            contact_ids.forEach(id => {
                queryClient.invalidateQueries({
                    queryKey: ["contacts", id]
                });
            });
            // The contacts table reads ["contacts","list",...]; refresh it so the
            // deleted rows disappear without a manual reload. ["campaigns"] carries
            // the lead counts the deleted contacts were part of.
            return Promise.all([
                queryClient.invalidateQueries({ queryKey: ["contacts", "list"] }),
                queryClient.invalidateQueries({ queryKey: ["campaigns"] }),
            ]);
        }
    })
}
