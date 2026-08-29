export default interface AddContact {
    first_name: string;
    last_name: string;
    email: string;
    company: string;
    phone: string;
    campaigns: string[];
    categories?: string[];

    custom_fields: Record<string, string>;

    // First-touch source hint. The dashboard may say "manual" or "campaign"
    // (added from a campaign's Leads tab); the server decides everything else.
    source?: "manual" | "campaign";
}
