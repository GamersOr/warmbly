// A hosted workspace without a subscription can manage mailboxes, its
// Warmbly Cloud links and settings; every other page renders behind the
// upgrade overlay until a plan is active.

import React from "react";
import { useLocation } from "react-router-dom";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import { LockedSurface } from "./LockedSurface";

const OPEN_PREFIXES = ["/app/emails", "/app/settings", "/app/select-org"];

export default function SubscriptionGate({ children }: { children: React.ReactNode }) {
    const access = useFeatureAccess();
    const { pathname } = useLocation();
    const open = OPEN_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + "/"));
    return (
        <LockedSurface
            locked={access.locked && !open}
            feature="Sending, inbox and CRM"
            blurb="Your workspace is on the free plan: connect up to 10 mailboxes and warm them, or link a self-hosted instance. Campaigns, the unified inbox, contacts, CRM and integrations unlock with a plan."
            minPlan="starter"
        >
            {children}
        </LockedSurface>
    );
}
