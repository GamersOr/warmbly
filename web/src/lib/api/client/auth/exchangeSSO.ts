import type Token from "../../models/auth/Token";
import Request from "../Request";

/**
 * Swaps the single-use code from an SSO redirect for the real session.
 *
 * The backend holds the session and hands back only an opaque code, so no token
 * lands in a URL, browser history or proxy log. `binding` proves this is the
 * browser that started the sign-in. The result is a login like any other, so it
 * can also come back as a 2FA challenge.
 */
export default async function exchangeSSO(
    code: string,
    binding: string,
): Promise<Token & { two_fa_required?: boolean; pending_token?: string }> {
    return await Request<Token & { two_fa_required?: boolean; pending_token?: string }>({
        method: "POST",
        url: "/auth/sso/exchange",
        data: { code, binding },
    });
}
