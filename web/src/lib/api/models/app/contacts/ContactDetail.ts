import type Contact from "./Contact";

export interface ContactEngagement {
    total_sent: number;
    total_opened: number;
    total_clicked: number;
    total_replied: number;
    total_bounced: number;
    total_complained: number;

    last_sent_at?: string | null;
    last_opened_at?: string | null;
    last_clicked_at?: string | null;
    last_replied_at?: string | null;
    last_bounced_at?: string | null;
}

export interface ContactSuppression {
    reason: string;
    source: "bounce" | "complaint" | "unsubscribe" | string;
    expires_at?: string | null;
    created_at: string;
}

// Where a contact first came from. Mirrors the backend CHECK; "unknown" is
// what rows created before attribution existed carry.
export type ContactSource =
    | "unknown"
    | "manual"
    | "campaign"
    | "import"
    | "sheet_sync"
    | "api"
    | "ai_assistant";

export default interface ContactDetail extends Contact {
    engagement: ContactEngagement;
    suppression?: ContactSuppression | null;

    // First-touch attribution; never changes after creation.
    source: ContactSource;
    source_detail: string;
    first_seen_at: string;
}
