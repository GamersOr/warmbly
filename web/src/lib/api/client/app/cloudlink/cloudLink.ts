// /cloud-link/* — the self-hosted instance's link to Warmbly Cloud.

import Request from "@/lib/api/client/Request";
import type {
    CloudLinkMailboxRow,
    CloudLinkPendingConnect,
    CloudLinkPollResult,
    CloudLinkStatus,
} from "@/lib/api/models/app/cloudlink/CloudLink";

export async function getCloudLinkStatus(): Promise<CloudLinkStatus> {
    return await Request<CloudLinkStatus>({ method: "GET", url: "/cloud-link", authorization: true });
}

export async function startCloudLinkConnect(cloudUrl?: string): Promise<CloudLinkPendingConnect> {
    return await Request<CloudLinkPendingConnect>({
        method: "POST",
        url: "/cloud-link/connect",
        data: { cloud_url: cloudUrl ?? "" },
        authorization: true,
    });
}

export async function pollCloudLinkConnect(): Promise<CloudLinkPollResult> {
    return await Request<CloudLinkPollResult>({ method: "POST", url: "/cloud-link/connect/poll", authorization: true });
}

export async function disconnectCloudLink(): Promise<void> {
    await Request<void>({ method: "DELETE", url: "/cloud-link", authorization: true });
}

export async function listCloudLinkMailboxes(): Promise<CloudLinkMailboxRow[]> {
    const res = await Request<{ data: CloudLinkMailboxRow[] }>({ method: "GET", url: "/cloud-link/mailboxes", authorization: true });
    return res.data ?? [];
}

export async function enrollCloudLinkMailbox(id: string): Promise<CloudLinkMailboxRow> {
    return await Request<CloudLinkMailboxRow>({ method: "POST", url: `/cloud-link/mailboxes/${id}/enroll`, authorization: true });
}

export async function unenrollCloudLinkMailbox(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/cloud-link/mailboxes/${id}/enroll`, authorization: true });
}

export async function setCloudLinkMailboxLifecycle(id: string, action: "pause" | "resume"): Promise<CloudLinkMailboxRow> {
    return await Request<CloudLinkMailboxRow>({ method: "POST", url: `/cloud-link/mailboxes/${id}/${action}`, authorization: true });
}
