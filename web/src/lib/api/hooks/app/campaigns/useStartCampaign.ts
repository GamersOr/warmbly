import { useMutation, useQueryClient } from "@tanstack/react-query";
import startCampaign, { type StartCampaignOptions } from "@/lib/api/client/app/campaigns/startCampaign";

export default function useStartCampaign() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (arg: string | { id: string; options?: StartCampaignOptions }) =>
            typeof arg === "string" ? startCampaign(arg) : startCampaign(arg.id, arg.options),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["campaigns"],
            })
        }
    })
}
