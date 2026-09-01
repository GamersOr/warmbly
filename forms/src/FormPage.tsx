// The /f/$publicId route: fetch the form, paint its theme, run the embed
// plumbing, render. The Go service already 404s unknown ids at the shell, so
// the in-app not-found path only fires when a form unpublishes mid-visit.

import { useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { fetchForm, FormNotFoundError } from "./api";
import { applyDesign, resolveDesign } from "./design";
import { isEmbedded, startResizeReporting } from "./embed";
import { FormRenderer } from "./FormRenderer";
import { NotFound, Unavailable } from "./NotFound";

export function FormPage() {
    const { publicId } = useParams({ from: "/f/$publicId" });
    const query = useQuery({
        queryKey: ["form", publicId],
        queryFn: () => fetchForm(publicId),
        staleTime: Infinity,
        retry: (count, err) => !(err instanceof FormNotFoundError) && count < 2,
    });

    const form = query.data;
    const design = useMemo(() => (form ? resolveDesign(form.design) : null), [form]);

    useEffect(() => {
        if (!form || !design) return;
        document.title = form.name;
        applyDesign(design);
        if (isEmbedded()) document.body.classList.add("embed");
        return startResizeReporting(form.public_id);
    }, [form, design]);

    if (query.isPending) return null;
    if (query.isError) {
        return query.error instanceof FormNotFoundError ? <NotFound /> : <Unavailable />;
    }
    if (!form || !design) return null;

    return (
        <main className="wf">
            <div className="card">
                <FormRenderer form={form} design={design} />
            </div>
            <div className="brand">
                <a href="https://warmbly.com" target="_blank" rel="noopener noreferrer">
                    Powered by Warmbly
                </a>
            </div>
        </main>
    );
}
