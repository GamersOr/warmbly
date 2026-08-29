import type { SearchContactsSortBy } from "./search-contacts.types";
import type SearchContactsFilter from "./SearchContactsFilter";
import type { LeadEngagement, LeadStatus } from "./Contact";

export default interface SearchContacts {
    query: string;
    filters: SearchContactsFilter[];
    campaign_ids: string[];
    // Campaign Leads view only (exactly one campaign_id): narrow to one derived
    // lead status and/or one engagement bucket. The server ANDs the two.
    lead_status?: LeadStatus;
    engagement?: LeadEngagement;
    category_ids?: string[];
    min_campaigns?: number;
    max_campaigns?: number;
    subscribed?: boolean;
    created_after?: Date;
    created_before?: Date;
    updated_after?: Date;
    updated_before?: Date;
    sort_by: SearchContactsSortBy;
    reverse: boolean;
}
