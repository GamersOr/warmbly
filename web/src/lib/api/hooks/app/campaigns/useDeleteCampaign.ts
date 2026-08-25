import deleteCampaign from "@/lib/api/client/app/campaigns/deleteCampaign";
import type GetCampaigns from "@/lib/api/models/app/campaigns/GetCampaigns";
import { type InfiniteData, useMutation, useQueryClient } from "@tanstack/react-query";

// Deletes a campaign. The row leaves every cached list at once so the
// campaigns page updates without a refetch. Its detail and analytics caches
// are dropped rather than invalidated (a refetch would only 404); the copy a
// mounted detail page still observes is left alone, since that page
// navigates away as soon as the delete resolves.
export default function useDeleteCampaign() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => deleteCampaign(id),
        onSuccess: (_data, id) => {
            const allLists = queryClient.getQueriesData<InfiniteData<GetCampaigns>>({
                queryKey: ["campaigns", "list"],
            });
            for (const [key, oldData] of allLists) {
                if (!oldData) continue;
                queryClient.setQueryData(key, {
                    ...oldData,
                    pages: oldData.pages.map((page) => ({
                        ...page,
                        data: page.data.filter((c) => c.id !== id),
                    })),
                });
            }
            queryClient.removeQueries({ queryKey: ["campaigns", id], type: "inactive" });
            queryClient.removeQueries({ queryKey: ["analytics", "campaigns", id], type: "inactive" });
            queryClient.invalidateQueries({ queryKey: ["campaigns", "list"] });
            queryClient.invalidateQueries({ queryKey: ["analytics"] });
            queryClient.invalidateQueries({ queryKey: ["contacts"] });
        },
    });
}
