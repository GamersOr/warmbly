// Pure helpers for the contact filter bar, kept out of the component file so
// fast refresh keeps working.

import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import type SearchContactsFilter from "@/lib/api/models/app/contacts/SearchContactsFilter";

export function isCompleteCustomFilter(f: SearchContactsFilter): boolean {
    return f.name.trim() !== "" && f.value.trim() !== "";
}

export function countActiveFilters(f: SearchContacts, campaignContext: boolean): number {
    let n = 0;
    n += f.filters.filter(isCompleteCustomFilter).length;
    if (f.category_ids?.length) n++;
    if (f.segment_ids?.length) n++;
    if (!campaignContext && f.campaign_ids.length > 0) n++;
    if (f.subscribed !== undefined) n++;
    if (f.min_campaigns !== undefined || f.max_campaigns !== undefined) n++;
    if (f.created_after || f.created_before) n++;
    if (f.updated_after || f.updated_before) n++;
    if (f.lead_status) n++;
    if (f.engagement) n++;
    return n;
}

