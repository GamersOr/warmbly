-- Where an open or click came from: the mail client or image proxy, the
-- browser, device and operating system parsed from the user agent, and the
-- country, region and city resolved from the source network. Clicks gain
-- these columns on the per-link log. Opens get their own log, because the
-- progress row keeps only the first open per step and a second open from
-- another device is worth seeing.
ALTER TABLE email_link_clicks
    ADD COLUMN client text NOT NULL DEFAULT '',
    ADD COLUMN device_type text NOT NULL DEFAULT '',
    ADD COLUMN os text NOT NULL DEFAULT '',
    ADD COLUMN browser text NOT NULL DEFAULT '',
    ADD COLUMN browser_version text NOT NULL DEFAULT '',
    ADD COLUMN country_code text NOT NULL DEFAULT '',
    ADD COLUMN region text NOT NULL DEFAULT '',
    ADD COLUMN city text NOT NULL DEFAULT '';

CREATE INDEX idx_email_link_clicks_campaign ON email_link_clicks (campaign_id, clicked_at DESC);

CREATE TABLE email_opens (
    id uuid PRIMARY KEY,
    task_id uuid NOT NULL,
    campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    contact_id uuid NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    sequence_id uuid NOT NULL REFERENCES sequences(id) ON DELETE CASCADE,
    opened_at timestamp with time zone NOT NULL,
    machine boolean NOT NULL DEFAULT false,
    machine_reason text NOT NULL DEFAULT '' CHECK (machine_reason IN ('', 'prefetch', 'instant')),
    user_agent text NOT NULL DEFAULT '',
    ip_hash text NOT NULL DEFAULT '',
    client text NOT NULL DEFAULT '',
    device_type text NOT NULL DEFAULT '',
    os text NOT NULL DEFAULT '',
    browser text NOT NULL DEFAULT '',
    browser_version text NOT NULL DEFAULT '',
    country_code text NOT NULL DEFAULT '',
    region text NOT NULL DEFAULT '',
    city text NOT NULL DEFAULT ''
);

CREATE INDEX idx_email_opens_contact ON email_opens (contact_id, opened_at DESC);
CREATE INDEX idx_email_opens_campaign ON email_opens (campaign_id, opened_at DESC);
CREATE INDEX idx_email_opens_step ON email_opens (campaign_id, contact_id, sequence_id);
