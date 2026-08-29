// Pin a workspace's posture to an operator's decision.
//
// The pin outranks the score and survives every later detector write, so
// clearing a workspace by review is not undone by the evidence still on file.
// The reason is shown to the workspace itself, which is why the field says so.

import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { setOrganizationRiskOverride } from "@/lib/api/client/admin/organizations";
import type { OrgRisk, OrgRiskState } from "@/lib/api/models/admin";

const BANDS: { state: OrgRiskState; effect: string }[] = [
    { state: "trusted", effect: "Nothing is restricted" },
    { state: "watch", effect: "Nothing the workspace can feel" },
    { state: "restricted", effect: "Quarter volume, free warmup pool" },
    { state: "suspended", effect: "Sending stops entirely" },
];

export function OrganizationRiskDialog({
    orgId,
    risk,
    open,
    onOpenChange,
}: {
    orgId: string;
    risk: OrgRisk;
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const qc = useQueryClient();
    const [state, setState] = useState<OrgRiskState>(risk.state);
    const [reason, setReason] = useState(risk.override?.reason ?? "");

    // Reseed against a fresh record each time it opens.
    useEffect(() => {
        if (open) {
            setState(risk.state);
            setReason(risk.override?.reason ?? "");
        }
    }, [open, risk.state, risk.override?.reason]);

    const mutation = useMutation({
        mutationFn: () => setOrganizationRiskOverride(orgId, { state, reason: reason.trim() }),
        onSuccess: (updated) => {
            qc.setQueryData(["admin", "organizations", orgId, "risk"], updated);
            qc.invalidateQueries({ queryKey: ["admin", "organizations", orgId] });
            qc.invalidateQueries({ queryKey: ["admin", "organizations"] });
            toast.success(`Posture pinned to ${updated.state}`);
            onOpenChange(false);
        },
        onError: (e: Error) => toast.error(e.message || "Failed to set the posture"),
    });

    function submit() {
        if (reason.trim() === "") {
            toast.error("A reason is required");
            return;
        }
        mutation.mutate();
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-lg">
                <DialogHeader>
                    <DialogTitle>Set workspace posture</DialogTitle>
                    <DialogDescription>
                        This pins the band. Detectors keep scoring the evidence, but the
                        posture stays where you put it until the override is lifted. The
                        score right now is <strong>{risk.score}</strong>.
                    </DialogDescription>
                </DialogHeader>

                <div className="grid gap-2">
                    {BANDS.map((b) => (
                        <label
                            key={b.state}
                            className={`flex items-center gap-3 rounded-md border p-2.5 cursor-pointer text-sm ${
                                state === b.state
                                    ? "border-[var(--admin-accent)] bg-muted/40"
                                    : "border-border hover:bg-muted/20"
                            }`}
                        >
                            <input
                                type="radio"
                                name="risk-state"
                                value={b.state}
                                checked={state === b.state}
                                onChange={() => setState(b.state)}
                            />
                            <span className="font-medium w-24">{b.state}</span>
                            <span className="text-xs text-muted-foreground">{b.effect}</span>
                        </label>
                    ))}

                    <div className="mt-2">
                        <Label htmlFor="risk-reason" className="text-xs font-medium">
                            Reason
                        </Label>
                        <Input
                            id="risk-reason"
                            placeholder="Why this workspace is where you are putting it"
                            value={reason}
                            onChange={(e) => setReason(e.target.value)}
                        />
                        <div className="text-[10px] text-muted-foreground mt-1">
                            Shown to the workspace in its dashboard banner, so write it for
                            them. Internal notes belong in the audit trail, not here.
                        </div>
                    </div>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        onClick={submit}
                        disabled={mutation.isPending}
                        className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                    >
                        {mutation.isPending ? "Saving…" : "Pin posture"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
