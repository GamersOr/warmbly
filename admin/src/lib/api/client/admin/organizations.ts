// /admin/organizations/* — workspace admin (read-only). List/detail/members
// only in this slice; write paths (overrides, ban scope) land in slice 2.

import { Request } from "@/lib/api/client";
import { buildSearchQuery } from "@/lib/api/client/admin/query";
import type {
    AdminOrgDetail,
    AdminOrgMembersResult,
    AdminOrgSearch,
    AdminOrgsResult,
    OrganizationLimitOverrides,
    OrgRisk,
    SetOrgRiskOverrideRequest,
    UpdateOrgOverridesRequest,
} from "@/lib/api/models/admin";

function toQuery(params: AdminOrgSearch): string {
    return buildSearchQuery(params as Record<string, unknown>);
}

export function listOrganizations(
    params: AdminOrgSearch = {},
): Promise<AdminOrgsResult> {
    return Request({
        method: "GET",
        url: `/admin/organizations${toQuery(params)}`,
        authorization: true,
    });
}

export function getOrganization(id: string): Promise<AdminOrgDetail> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}`,
        authorization: true,
    });
}

export function getOrganizationMembers(
    id: string,
): Promise<AdminOrgMembersResult> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}/members`,
        authorization: true,
    });
}

export function getOrganizationOverrides(
    id: string,
): Promise<OrganizationLimitOverrides | null> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}/overrides`,
        authorization: true,
    });
}

export function updateOrganizationOverrides(
    id: string,
    body: UpdateOrgOverridesRequest,
): Promise<OrganizationLimitOverrides> {
    return Request({
        method: "PUT",
        url: `/admin/organizations/${id}/overrides`,
        authorization: true,
        data: body,
    });
}

/** The posture with its evidence. The customer endpoint withholds the signal
 *  blob, so this is the only place the reason a workspace was flagged is
 *  readable. */
export function getOrganizationRisk(id: string): Promise<OrgRisk> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${id}/risk`,
        authorization: true,
    });
}

/** Pin the posture. The pin outranks the score and survives every later
 *  detector write, until it is lifted. */
export function setOrganizationRiskOverride(
    id: string,
    body: SetOrgRiskOverrideRequest,
): Promise<OrgRisk> {
    return Request({
        method: "PUT",
        url: `/admin/organizations/${id}/risk`,
        authorization: true,
        data: body,
    });
}

/** Lift the pin and hand the posture back to the evidence. */
export function clearOrganizationRiskOverride(id: string): Promise<OrgRisk> {
    return Request({
        method: "DELETE",
        url: `/admin/organizations/${id}/risk`,
        authorization: true,
    });
}

/** Retract one detector's finding, which lowers the score for good. */
export function clearOrganizationRiskSignal(
    id: string,
    key: string,
): Promise<OrgRisk> {
    return Request({
        method: "DELETE",
        url: `/admin/organizations/${id}/risk/signals/${encodeURIComponent(key)}`,
        authorization: true,
    });
}
