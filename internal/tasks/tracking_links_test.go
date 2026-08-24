package tasks

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAddOpenTrackingPixelUsesTheConfiguredHost(t *testing.T) {
	html := "<html><body>hi</body></html>"
	out := AddOpenTrackingPixel(html, uuid.New(), "t.acme.com")
	if !strings.Contains(out, `src="https://t.acme.com/t/o/`) {
		t.Fatalf("pixel did not use the host: %s", out)
	}
}

// A local tracking service does not serve TLS, so an https pixel there is one
// no mail client can load.
func TestAddOpenTrackingPixelUsesHTTPForLocalHosts(t *testing.T) {
	out := AddOpenTrackingPixel("<html><body>hi</body></html>", uuid.New(), "localhost:3000")
	if !strings.Contains(out, `src="http://localhost:3000/t/o/`) {
		t.Fatalf("expected an http pixel, got: %s", out)
	}
}

// Without a tracking host there is nowhere to record an open. The pixel used to
// fall back to a hardcoded warmbly.com name, which on any other install meant
// mailing recipients a beacon to a third party that recorded nothing locally.
func TestAddOpenTrackingPixelSkippedWithoutAHost(t *testing.T) {
	html := "<html><body>hi</body></html>"
	if out := AddOpenTrackingPixel(html, uuid.New(), ""); out != html {
		t.Fatalf("expected the body untouched, got: %s", out)
	}
}

func TestWrapLinksForTrackingMintsTickets(t *testing.T) {
	html := `<a href="https://example.com/pricing">pricing</a>`
	out, links := WrapLinksForTracking(html, uuid.New(), uuid.New(), "t.acme.com")
	if len(links) != 1 {
		t.Fatalf("expected one ticket, got %d", len(links))
	}
	if links[0].Destination != "https://example.com/pricing" {
		t.Fatalf("destination not preserved: %q", links[0].Destination)
	}
	if !strings.Contains(out, "https://t.acme.com/c/"+links[0].ID.String()) {
		t.Fatalf("link not rewritten to the ticket: %s", out)
	}
}

// The worst outcome of a missing tracking host: every link in a real campaign
// email rewritten to a host that does not exist. Ship the originals instead.
func TestWrapLinksForTrackingLeavesLinksAloneWithoutAHost(t *testing.T) {
	html := `<a href="https://example.com/pricing">pricing</a>`
	out, links := WrapLinksForTracking(html, uuid.New(), uuid.New(), "")
	if out != html || links != nil {
		t.Fatalf("expected the original links, got %s / %v", out, links)
	}
}

func TestWrapLinksForTrackingSkipsItsOwnHostAndNonHTTP(t *testing.T) {
	html := `<a href="https://t.acme.com/c/abc">x</a><a href="mailto:a@b.com">m</a><a href="#top">t</a>`
	out, links := WrapLinksForTracking(html, uuid.New(), uuid.New(), "t.acme.com")
	if len(links) != 0 {
		t.Fatalf("expected no tickets, got %d", len(links))
	}
	if out != html {
		t.Fatalf("body should be unchanged: %s", out)
	}
}
