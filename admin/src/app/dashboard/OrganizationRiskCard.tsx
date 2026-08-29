// Workspace abuse posture — the operator view. The customer endpoint returns
// the band and a sentence; this is where the evidence behind it is readable,
// and the only place a posture can be changed or lifted.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Pin, PinOff, ShieldAlert, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import {
    clearOrganizationRiskOverride,
    clearOrganizationRiskSignal,
    getOrganizationRisk,
} from "@/lib/api/client/admin/organizations";
import type { OrgRisk, OrgRiskSignal, OrgRiskState } from "@/lib/api/models/admin";
import { OrganizationRiskDialog } from "./OrganizationRiskDialog";

/** Band colours, in one place so the pill reads the same everywhere. */
const BAND_STYLES: Record<OrgRiskState, string> = {
    trusted: "border-emerald-300 text-emerald-700 bg-emerald-50",
    watch: "border-slate-300 text-slate-700 bg-slate-50",
    restricted: "border-amber-300 text-amber-700 bg-amber-50",
    suspended: "border-red-300 text-red-700 bg-red-50",
};

const BAND_EFFECT: Record<OrgRiskState, string> = {
    trusted: "Nothing is restricted.",
    watch: "Nothing the workspace can feel. Evidence is accumulating.",
    restricted: "Daily volume per mailbox is a quarter, and warmup is on the free pool.",
    suspended: "Sending is stopped entirely.",
};

export function RiskBadge({ state }: { state: OrgRiskState }) {
    return (
        <Badge variant="outline" className={`text-[10px] ${BAND_STYLES[state]}`}>
            {state}
        </Badge>
    );
}

export function OrganizationRiskCard({ orgId }: { orgId: string }) {
    const qc = useQueryClient();
    const [editing, setEditing] = useState(false);
    // Reading the evidence needs only view_organizations; every write below is
    // gated on manage_organizations, so an admin without it is shown the
    // record rather than buttons that can only answer 403.
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);

    const riskQuery = useQuery({
        queryKey: ["admin", "organizations", orgId, "risk"],
        queryFn: () => getOrganizationRisk(orgId),
        enabled: !!orgId,
    });

    // Both writes return the whole record, so seed the cache from the response
    // rather than refetching, and refresh the org row whose band is inlined.
    function applied(message: string) {
        return (risk: OrgRisk) => {
            qc.setQueryData(["admin", "organizations", orgId, "risk"], risk);
            qc.invalidateQueries({ queryKey: ["admin", "organizations", orgId] });
            qc.invalidateQueries({ queryKey: ["admin", "organizations"] });
            toast.success(message);
        };
    }

    const liftMutation = useMutation({
        mutationFn: () => clearOrganizationRiskOverride(orgId),
        onSuccess: applied("Override lifted; the evidence decides again"),
        onError: (e: Error) => toast.error(e.message || "Failed to lift the override"),
    });

    const retractMutation = useMutation({
        mutationFn: (key: string) => clearOrganizationRiskSignal(orgId, key),
        onSuccess: applied("Finding retracted"),
        onError: (e: Error) => toast.error(e.message || "Failed to retract the finding"),
    });

    if (riskQuery.isLoading) return <Skeleton className="h-40 w-full" />;
    if (riskQuery.error || !riskQuery.data) {
        return (
            <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                Failed to load the workspace posture.
            </div>
        );
    }

    const risk = riskQuery.data;
    const signals = Object.entries(risk.signals ?? {});
    const pinned = risk.override ?? null;

    return (
        <div className="border border-border rounded-lg bg-card">
            <div className="flex items-start justify-between gap-3 p-3 border-b border-border">
                <div>
                    <div className="flex items-center gap-2">
                        <RiskBadge state={risk.state} />
                        <span className="text-sm font-medium tabular-nums">
                            score {risk.score}
                        </span>
                        {pinned && (
                            <span className="inline-flex items-center gap-1 text-[10px] text-[var(--admin-accent-strong)]">
                                <Pin className="size-3" /> pinned by an operator
                            </span>
                        )}
                    </div>
                    <div className="text-xs text-muted-foreground mt-1">
                        {BAND_EFFECT[risk.state]}
                    </div>
                    {risk.reason && (
                        <div className="text-xs mt-1">
                            <span className="text-muted-foreground">Shown to the workspace: </span>
                            {risk.reason}
                        </div>
                    )}
                    {risk.evaluated_at && (
                        <div className="text-[10px] text-muted-foreground mt-1">
                            Last evaluated {new Date(risk.evaluated_at).toLocaleString()}
                        </div>
                    )}
                </div>
                {canManage && (
                    <div className="flex items-center gap-2 shrink-0">
                        {pinned && (
                            <Button
                                size="sm"
                                variant="outline"
                                onClick={() => liftMutation.mutate()}
                                disabled={liftMutation.isPending}
                            >
                                <PinOff className="size-3.5" />
                                {liftMutation.isPending ? "Lifting…" : "Lift override"}
                            </Button>
                        )}
                        <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
                            <ShieldAlert className="size-3.5" />
                            Set posture
                        </Button>
                    </div>
                )}
            </div>

            {pinned && (
                <div className="px-3 py-2 border-b border-border bg-muted/30 text-xs">
                    Pinned to <strong>{pinned.state}</strong>
                    {pinned.at && <> on {new Date(pinned.at).toLocaleString()}</>}
                    {pinned.reason && <> — "{pinned.reason}"</>}
                    <div className="text-[10px] text-muted-foreground mt-0.5">
                        Detectors keep scoring the evidence, but the band stays here until
                        the override is lifted.
                    </div>
                </div>
            )}

            <SignalsTable
                signals={signals}
                onRetract={canManage ? (key) => retractMutation.mutate(key) : undefined}
                busyKey={retractMutation.isPending ? retractMutation.variables : undefined}
            />

            {canManage && (
                <OrganizationRiskDialog
                    orgId={orgId}
                    risk={risk}
                    open={editing}
                    onOpenChange={setEditing}
                />
            )}
        </div>
    );
}

function SignalsTable({
    signals,
    onRetract,
    busyKey,
}: {
    signals: [string, OrgRiskSignal][];
    // Absent when the admin can read the evidence but not change it.
    onRetract?: (key: string) => void;
    busyKey?: string;
}) {
    if (signals.length === 0) {
        return (
            <div className="p-3 text-xs text-muted-foreground">
                No detector has filed anything against this workspace.
            </div>
        );
    }
    // Heaviest first, matching the sentence the customer is shown.
    const rows = [...signals].sort(
        (a, b) => (b[1].weight ?? 0) - (a[1].weight ?? 0),
    );
    return (
        <table className="w-full text-sm">
            <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                <tr>
                    <th className="text-left px-3 py-2 font-medium">Detector</th>
                    <th className="text-right px-3 py-2 font-medium">Weight</th>
                    <th className="text-left px-3 py-2 font-medium">Finding</th>
                    <th className="text-left px-3 py-2 font-medium">Ages out</th>
                    {onRetract && <th className="px-3 py-2 w-8" />}
                </tr>
            </thead>
            <tbody>
                {rows.map(([key, signal]) => {
                    const expires = signal.expires_at
                        ? new Date(signal.expires_at)
                        : null;
                    const expired = expires ? expires.getTime() < Date.now() : false;
                    return (
                        <tr key={key} className="border-t border-border align-top">
                            <td className="px-3 py-2 font-medium">{key}</td>
                            <td className="px-3 py-2 text-right tabular-nums">
                                {signal.weight ?? 0}
                            </td>
                            <td className="px-3 py-2 text-muted-foreground">
                                {signal.detail || "—"}
                            </td>
                            <td className="px-3 py-2 text-xs text-muted-foreground">
                                {expires ? (
                                    expired ? (
                                        <span className="text-amber-700">
                                            expired, awaiting sweep
                                        </span>
                                    ) : (
                                        expires.toLocaleDateString()
                                    )
                                ) : (
                                    "only when retracted"
                                )}
                            </td>
                            {onRetract && (
                                <td className="px-3 py-2">
                                    <button
                                        type="button"
                                        title="Retract this finding"
                                        onClick={() => onRetract(key)}
                                        disabled={busyKey === key}
                                        className="text-muted-foreground hover:text-red-600 disabled:opacity-40"
                                    >
                                        <X className="size-3.5" />
                                    </button>
                                </td>
                            )}
                        </tr>
                    );
                })}
            </tbody>
        </table>
    );
}
