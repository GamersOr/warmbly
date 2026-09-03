/**
 * The running Warmbly on a self-hosted instance and whether a newer one
 * exists, served by GET /auth/instance. A hosted deployment answers
 * `self_hosted: false` and nothing else, so the dashboard shows no pill there.
 */
export default interface InstanceVersion {
    self_hosted: boolean;
    version?: string;
    commit?: string;
    update_available: boolean;
    latest?: {
        tag: string;
        html_url?: string;
        published_at?: string;
    };
    checked_at?: string;
}
