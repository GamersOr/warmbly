// Terminal states, word-for-word with the static pages the Go service serves
// for the same conditions at the shell level.

export function NotFound() {
    return (
        <div className="plain">
            <h1>This form is no longer available</h1>
            <p>It may have been unpublished or removed.</p>
        </div>
    );
}

export function Unavailable() {
    return (
        <div className="plain">
            <h1>This form is temporarily unavailable</h1>
            <p>Please try again in a moment.</p>
        </div>
    );
}
