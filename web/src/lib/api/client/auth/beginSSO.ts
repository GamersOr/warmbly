import Request from "../Request";

/**
 * Starts a browser sign-in with one provider: "oidc", "google" or "apple".
 *
 * The backend returns the authorization URL rather than redirecting, because
 * the dashboard is a single-page app on a different origin from the API. The
 * state, nonce and PKCE verifier are minted and held server-side, so nothing
 * the client holds can be replayed.
 */
export default async function beginSSO(provider: string): Promise<{ url: string }> {
    return await Request<{ url: string }>({
        method: "POST",
        url: `/auth/${provider}/begin`,
    });
}
