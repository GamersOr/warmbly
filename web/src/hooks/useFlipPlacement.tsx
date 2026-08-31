import React from "react";

/**
 * Decides whether a popup should render above or below its trigger
 * based on available room inside the nearest scrolling/clipping
 * ancestor. Use when an absolute-positioned dropdown lives inside a
 * container with overflow:auto|hidden|scroll (modal body, slide-over,
 * etc.) where a too-tall dropdown would get clipped.
 *
 * Measures on open. Cheap — getBoundingClientRect once per ancestor.
 */
export default function useFlipPlacement(
    triggerRef: React.RefObject<HTMLElement | null>,
    open: boolean,
    estimatedHeight: number,
): "top" | "bottom" {
    const [placement, setPlacement] = React.useState<"top" | "bottom">("bottom");

    React.useLayoutEffect(() => {
        if (!open) return;
        const trigger = triggerRef.current;
        if (!trigger) return;

        const rect = trigger.getBoundingClientRect();
        let clipBottom = window.innerHeight;
        let clipTop = 0;
        let el: HTMLElement | null = trigger.parentElement;
        while (el) {
            const s = getComputedStyle(el);
            const overflow = `${s.overflow} ${s.overflowY} ${s.overflowX}`;
            if (/(auto|scroll|hidden)/.test(overflow)) {
                const ar = el.getBoundingClientRect();
                if (ar.bottom < clipBottom) clipBottom = ar.bottom;
                if (ar.top > clipTop) clipTop = ar.top;
            }
            el = el.parentElement;
        }

        const spaceBelow = clipBottom - rect.bottom;
        const spaceAbove = rect.top - clipTop;
        if (spaceBelow < estimatedHeight && spaceAbove > spaceBelow) {
            setPlacement("top");
        } else {
            setPlacement("bottom");
        }
    }, [open, triggerRef, estimatedHeight]);

    return placement;
}

/**
 * Horizontal counterpart: decides whether a popup should hang from the
 * trigger's left or right edge, based on room inside the nearest
 * clipping ancestor. Use when the trigger can land anywhere along a
 * wrapping toolbar row, where a fixed alignment pushes the popup out of
 * the container on one side or the other.
 */
export function useFlipAlignment(
    triggerRef: React.RefObject<HTMLElement | null>,
    open: boolean,
    estimatedWidth: number,
): "left" | "right" {
    const [alignment, setAlignment] = React.useState<"left" | "right">("left");

    React.useLayoutEffect(() => {
        if (!open) return;
        const trigger = triggerRef.current;
        if (!trigger) return;

        const rect = trigger.getBoundingClientRect();
        let clipRight = window.innerWidth;
        let clipLeft = 0;
        let el: HTMLElement | null = trigger.parentElement;
        while (el) {
            const s = getComputedStyle(el);
            const overflow = `${s.overflow} ${s.overflowY} ${s.overflowX}`;
            if (/(auto|scroll|hidden)/.test(overflow)) {
                const ar = el.getBoundingClientRect();
                if (ar.right < clipRight) clipRight = ar.right;
                if (ar.left > clipLeft) clipLeft = ar.left;
            }
            el = el.parentElement;
        }

        // left-aligned grows rightward from the trigger's left edge;
        // right-aligned grows leftward from its right edge.
        const spaceRight = clipRight - rect.left;
        const spaceLeft = rect.right - clipLeft;
        if (spaceRight < estimatedWidth && spaceLeft > spaceRight) {
            setAlignment("right");
        } else {
            setAlignment("left");
        }
    }, [open, triggerRef, estimatedWidth]);

    return alignment;
}
