import type { SendLifecycleState } from "@/lib/api/models/app/analytics/AccountStatus";
import Request from "../../Request";

// Hold a mailbox out of campaign sending (reserve) or release it back into
// rotation. Bodyless and idempotent; returns the mailbox's lifecycle state.
export default async function sendHold(id: string, hold: boolean): Promise<SendLifecycleState> {
    return await Request<SendLifecycleState>({
        method: "POST",
        url: `/emails/${id}/${hold ? "hold" : "release"}`,
        authorization: true,
    });
}
