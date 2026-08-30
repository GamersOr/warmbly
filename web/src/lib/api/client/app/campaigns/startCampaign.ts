import Request from "../../Request";

export interface StartCampaignOptions {
    // Launch past the bounce-risk gate after reading the projection.
    acknowledge_list_risk?: boolean;
}

export default async function startCampaign(id: string, options?: StartCampaignOptions): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: `/campaigns/${id}/start`,
        data: options ?? {},
        authorization: true,
    })
}
