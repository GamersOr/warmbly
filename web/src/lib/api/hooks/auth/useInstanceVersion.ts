import { useQuery } from "@tanstack/react-query";
import getInstanceVersion from "../../client/auth/getInstanceVersion";

// Feeds the version pill in the header. The backend re-checks releases on its
// own interval, so a five minute poll here is enough for the pill to turn
// amber while the tab is open. A backend without the endpoint (an older
// self-host) fails once and the pill stays hidden.
export default function useInstanceVersion() {
    return useQuery({
        queryKey: ["auth", "instance"],
        queryFn: getInstanceVersion,
        staleTime: 5 * 60_000,
        refetchInterval: 5 * 60_000,
        retry: false,
    });
}
