// Only SMTP/IMAP mailboxes can be warmed by the cloud: a Google or Microsoft
// refresh grant is bound to the OAuth app that issued it, which is this
// instance's own, so the cloud could never refresh it.
export function providerSupported(provider: string): boolean {
    return provider === "smtp_imap";
}

export function providerLabel(provider: string): string {
    switch (provider) {
        case "gmail":
            return "Google";
        case "outlook":
            return "Microsoft";
        case "smtp_imap":
            return "SMTP/IMAP";
        default:
            return provider;
    }
}
