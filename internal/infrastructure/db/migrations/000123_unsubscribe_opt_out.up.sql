-- A campaign can override the workspace's in-body opt-out (text line, link,
-- or none); 'inherit' follows Settings > Sending.
ALTER TABLE campaigns ADD COLUMN unsubscribe_mode text NOT NULL DEFAULT 'inherit';
ALTER TABLE campaigns ADD CONSTRAINT campaigns_unsubscribe_mode_check
    CHECK (unsubscribe_mode IN ('inherit', 'text', 'link', 'off'));

-- The suppression list takes whole domains as well as addresses. A domain
-- row keeps the bare host in email ("acme.com") so the existing unique key
-- and index apply unchanged.
ALTER TABLE suppressed_recipients ADD COLUMN kind text NOT NULL DEFAULT 'email';
ALTER TABLE suppressed_recipients ADD CONSTRAINT suppressed_recipients_kind_check
    CHECK (kind IN ('email', 'domain'));

-- One predicate for every send gate, count and filter, so a domain entry is
-- honoured everywhere an address entry is. Written as a single SELECT so the
-- planner inlines it like the subqueries it replaces.
CREATE OR REPLACE FUNCTION recipient_suppressed(org uuid, addr text) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM suppressed_recipients sr
        WHERE sr.organization_id = org
          AND (sr.expires_at IS NULL OR sr.expires_at > now())
          AND (
                (sr.kind = 'email' AND lower(sr.email) = lower(addr))
             OR (sr.kind = 'domain' AND sr.email = split_part(lower(addr), '@', 2))
          )
    )
$$;
