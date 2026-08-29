use axum::{
    body::Bytes,
    extract::{Path, State},
    http::{header, HeaderMap, StatusCode},
    response::{IntoResponse, Redirect, Response},
};
use chrono::Utc;
use moka::future::Cache;
use sha2::{Digest, Sha256};
use std::sync::Arc;
use std::time::Duration;

use crate::abuse::{is_prefetch, is_scanner, RateLimiter};
use crate::config::Config;
use crate::events::TrackingEvent;
use crate::hits::{ForwardedHit, HitForwarder, HitPayload, Outcome};
use crate::links::{LinkResolver, Resolution};
use crate::producer::Producer;

// 1x1 transparent GIF (43 bytes)
const TRANSPARENT_GIF: &[u8] = &[
    0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0xFF, 0xFF, 0xFF,
    0x00, 0x00, 0x00, 0x21, 0xF9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2C, 0x00, 0x00, 0x00, 0x00,
    0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3B,
];

/// Cache key format: {event_type}:{task_id}:{ip_hash}
/// Prevents duplicate events from the same IP within a time window
type DedupeCache = Cache<String, ()>;

#[derive(Clone)]
pub struct AppState {
    pub producer: Producer,
    /// Cache to deduplicate tracking events
    /// Each event type + task + IP is cached for 1 hour
    pub dedupe_cache: Arc<DedupeCache>,
    /// Per-source request budget (anti-flood)
    pub rate_limiter: Arc<RateLimiter>,
    /// Click-ticket resolver (backend internal API + layered caches)
    pub links: Arc<LinkResolver>,
    /// Website page-view forwarder (backend internal API + layered caches)
    pub hits: Arc<HitForwarder>,
    /// Tighter per-source budget for page views than for pixels
    pub hit_rate_limiter: Arc<RateLimiter>,
}

impl AppState {
    pub fn new(producer: Producer, config: &Config) -> Self {
        // Create cache with:
        // - Max 100k entries
        // - TTL of 1 hour per entry
        // - TTI (time to idle) of 30 minutes
        let dedupe_cache = Cache::builder()
            .max_capacity(100_000)
            .time_to_live(Duration::from_secs(3600)) // 1 hour
            .time_to_idle(Duration::from_secs(1800)) // 30 min idle
            .build();

        Self {
            producer,
            dedupe_cache: Arc::new(dedupe_cache),
            rate_limiter: Arc::new(RateLimiter::new(config.rate_limit_per_min)),
            links: Arc::new(LinkResolver::new(
                config.backend_internal_url.clone(),
                config.internal_api_token.clone(),
            )),
            hits: Arc::new(HitForwarder::new(
                config.backend_internal_url.clone(),
                config.internal_api_token.clone(),
            )),
            hit_rate_limiter: Arc::new(RateLimiter::new(config.pagehit_rate_limit_per_min)),
        }
    }

    /// Check if this event was already processed (returns true if duplicate)
    async fn is_duplicate(
        &self,
        event_type: &str,
        task_id: &str,
        ip_hash: &Option<String>,
    ) -> bool {
        let key = format!(
            "{}:{}:{}",
            event_type,
            task_id,
            ip_hash.as_deref().unwrap_or("unknown")
        );

        // Check if exists, if not insert
        if self.dedupe_cache.contains_key(&key) {
            return true;
        }

        // Insert into cache
        self.dedupe_cache.insert(key, ()).await;
        false
    }
}

/// Health check endpoint
pub async fn health() -> impl IntoResponse {
    (StatusCode::OK, "OK")
}

/// Open tracking pixel handler
/// GET /t/o/{task_id}.png
pub async fn track_open(
    State(state): State<AppState>,
    Path(task_id): Path<String>,
    headers: HeaderMap,
) -> Response {
    // Remove .png suffix if present
    let task_id = task_id.trim_end_matches(".png").to_string();

    // Validate task_id is a valid UUID format
    if uuid::Uuid::parse_str(&task_id).is_err() {
        return pixel_response();
    }

    // Extract IP hash for deduplication + rate limiting
    let ip_hash = extract_ip_hash(&headers);

    // Anti-flood: over-budget sources still get the pixel (real mail clients
    // must never see a broken image), but nothing is published.
    let source = ip_hash.clone().unwrap_or_else(|| "unknown".to_string());
    if !state.rate_limiter.allow(&source).await {
        return pixel_response();
    }

    // Extract metadata from request
    let user_agent = headers
        .get(header::USER_AGENT)
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string());

    // Speculative fetches and scanners are served but never counted.
    if is_prefetch(&headers) || is_scanner(user_agent.as_deref()) {
        return pixel_response();
    }

    // Check for duplicate (same task + IP within 1 hour)
    if state.is_duplicate("OPEN", &task_id, &ip_hash).await {
        // Still return pixel but don't publish event
        return pixel_response();
    }

    // Publish event asynchronously (fire and forget)
    let producer = state.producer.clone();
    tokio::spawn(async move {
        producer
            .publish(TrackingEvent {
                event_type: "EMAIL_OPENED".to_string(),
                task_id,
                original_url: None,
                timestamp: Utc::now().to_rfc3339(),
                user_agent,
                ip_hash,
            })
            .await;
    });

    pixel_response()
}

/// Click tracking redirect handler
/// GET /c/{link_id}
///
/// The email carries only this opaque ticket; the destination lives
/// server-side, so there is nothing to forge and no open-redirect surface.
/// Unknown tickets 404.
pub async fn track_click(
    State(state): State<AppState>,
    Path(link_id): Path<String>,
    headers: HeaderMap,
) -> Response {
    // Garbage dies before any lookup or counter work
    if uuid::Uuid::parse_str(&link_id).is_err() {
        return (StatusCode::NOT_FOUND, "Unknown link").into_response();
    }

    // Anti-flood: cap total request rate per source
    let ip_hash = extract_ip_hash(&headers);
    let source = ip_hash.clone().unwrap_or_else(|| "unknown".to_string());
    if !state.rate_limiter.allow(&source).await {
        return (StatusCode::TOO_MANY_REQUESTS, "Slow down").into_response();
    }

    let link = match state.links.resolve(&link_id, &source).await {
        Resolution::Found(link) => link,
        Resolution::NotFound => {
            return (StatusCode::NOT_FOUND, "Unknown link").into_response();
        }
        Resolution::Unavailable => {
            // Fail closed: never redirect a ticket we could not verify.
            return (StatusCode::SERVICE_UNAVAILABLE, "Try again shortly").into_response();
        }
    };

    // Extract metadata from request
    let user_agent = headers
        .get(header::USER_AGENT)
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string());

    // Security gateways and link previewers follow every URL in a message;
    // serve them the destination but never count a click, and never hand
    // them the identification ticket.
    if is_prefetch(&headers) || is_scanner(user_agent.as_deref()) {
        return Redirect::temporary(&link.destination).into_response();
    }

    // When the workspace registered the destination's host for website
    // tracking, the ticket rides along so the snippet can tie the browser to
    // the recipient. The ticket is opaque and per-recipient: it names no
    // destination and no secret, only "the click the backend already knows".
    let target = if link.identify {
        with_identify_param(&link.destination, &link_id)
    } else {
        link.destination.clone()
    };

    // Dedupe repeat clicks of the same ticket from the same source
    if state.is_duplicate("CLICK", &link_id, &ip_hash).await {
        return Redirect::temporary(&target).into_response();
    }

    // Publish event asynchronously (fire and forget)
    let producer = state.producer.clone();
    let destination = link.destination.clone();
    tokio::spawn(async move {
        producer
            .publish(TrackingEvent {
                event_type: "EMAIL_CLICKED".to_string(),
                task_id: link.task_id,
                original_url: Some(destination),
                timestamp: Utc::now().to_rfc3339(),
                user_agent,
                ip_hash,
            })
            .await;
    });

    Redirect::temporary(&target).into_response()
}

/// Query parameter the click redirect appends and the snippet strips.
const IDENTIFY_PARAM: &str = "wbly_t";

/// Appends the identification ticket to a destination, keeping any existing
/// query and fragment intact.
fn with_identify_param(destination: &str, ticket: &str) -> String {
    let (base, fragment) = match destination.split_once('#') {
        Some((b, f)) => (b, Some(f)),
        None => (destination, None),
    };
    let sep = if base.contains('?') { '&' } else { '?' };
    let mut out = format!("{}{}{}={}", base, sep, IDENTIFY_PARAM, ticket);
    if let Some(f) = fragment {
        out.push('#');
        out.push_str(f);
    }
    out
}

/// The tracking snippet customers embed.
/// GET /tracking.js
pub async fn tracking_js() -> Response {
    (
        StatusCode::OK,
        [
            (
                header::CONTENT_TYPE,
                "application/javascript; charset=utf-8",
            ),
            (header::CACHE_CONTROL, "public, max-age=3600"),
        ],
        TRACKING_JS,
    )
        .into_response()
}

const TRACKING_JS: &str = include_str!("../static/tracking.js");

/// Website page-view ingest.
/// POST /p  (JSON body, sent as text/plain so browsers skip the preflight)
///
/// Same controls as the pixel, then the payload is validated and forwarded to
/// the backend, which owns consent policy, enrichment and storage. Nothing in
/// the URL, and nothing in the body names a contact.
pub async fn track_page_hit(
    State(state): State<AppState>,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    let ip = extract_ip(&headers);
    let ip_hash = ip.as_deref().map(hash_ip);
    let source = ip_hash.clone().unwrap_or_else(|| "unknown".to_string());

    // Anti-flood: page views have their own, tighter budget on top of the
    // shared one, and the shared one counts too so a flood here also
    // throttles the same source's pixels and clicks.
    if !state.rate_limiter.allow(&source).await || !state.hit_rate_limiter.allow(&source).await {
        return (StatusCode::TOO_MANY_REQUESTS, "Slow down").into_response();
    }

    let user_agent = headers
        .get(header::USER_AGENT)
        .and_then(|h| h.to_str().ok())
        .map(|s| s.to_string());

    // Prefetches, crawlers, and browsers signalling Global Privacy Control
    // are acknowledged and never counted.
    if is_prefetch(&headers) || is_scanner(user_agent.as_deref()) || has_gpc(&headers) {
        return StatusCode::NO_CONTENT.into_response();
    }

    let payload: HitPayload = match serde_json::from_slice(&body) {
        Ok(p) => p,
        Err(_) => return (StatusCode::BAD_REQUEST, "Invalid payload").into_response(),
    };
    if !payload.validate() {
        return (StatusCode::BAD_REQUEST, "Invalid payload").into_response();
    }

    // Reloads and double-fires: acknowledged, not counted.
    if state.hits.is_duplicate(&payload.v, &payload.u).await {
        return StatusCode::NO_CONTENT.into_response();
    }

    let origin_host = headers
        .get(header::ORIGIN)
        .or_else(|| headers.get(header::REFERER))
        .and_then(|h| h.to_str().ok())
        .map(host_of)
        .unwrap_or_default();

    let hit = ForwardedHit {
        site_key: payload.k,
        visitor_key: payload.v,
        session_key: payload.s,
        consent: payload.c,
        identify_token: payload.t,
        url: payload.u,
        title: payload.ti,
        referrer: payload.r,
        language: payload.l,
        timezone: payload.tz,
        screen_width: payload.sw,
        screen_height: payload.sh,
        landing: payload.ld,
        user_agent: user_agent.unwrap_or_default(),
        ip: ip.unwrap_or_default(),
        origin_host,
    };

    match state.hits.forward(hit, &source).await {
        Outcome::Accepted(None) => StatusCode::NO_CONTENT.into_response(),
        Outcome::Accepted(Some(vid)) => (
            StatusCode::OK,
            [(header::CONTENT_TYPE, "application/json")],
            format!("{{\"vid\":\"{}\"}}", vid),
        )
            .into_response(),
        // An unknown key is not told apart from a declined hit: the browser
        // learns nothing about which keys exist.
        Outcome::UnknownSite => StatusCode::NO_CONTENT.into_response(),
        Outcome::Malformed => (StatusCode::BAD_REQUEST, "Invalid payload").into_response(),
        Outcome::Unavailable => {
            (StatusCode::SERVICE_UNAVAILABLE, "Try again shortly").into_response()
        }
    }
}

/// Sec-GPC: 1 is the browser's own opt-out signal.
fn has_gpc(headers: &HeaderMap) -> bool {
    headers
        .get("sec-gpc")
        .and_then(|h| h.to_str().ok())
        .map(|v| v.trim() == "1")
        .unwrap_or(false)
}

/// Bare host of an Origin/Referer value ("https://www.example.com/x" ->
/// "www.example.com").
fn host_of(value: &str) -> String {
    let rest = value.split_once("://").map(|(_, r)| r).unwrap_or(value);
    rest.split(['/', '?', '#'])
        .next()
        .unwrap_or("")
        .to_ascii_lowercase()
}

/// Return the transparent pixel response
fn pixel_response() -> Response {
    (
        StatusCode::OK,
        [
            (header::CONTENT_TYPE, "image/gif"),
            (header::CACHE_CONTROL, "no-cache, no-store, must-revalidate"),
            (header::PRAGMA, "no-cache"),
            (header::EXPIRES, "0"),
        ],
        TRANSPARENT_GIF,
    )
        .into_response()
}

/// Extract and hash IP address for privacy
fn extract_ip_hash(headers: &HeaderMap) -> Option<String> {
    extract_ip(headers).map(|ip| hash_ip(&ip))
}

/// The client IP as the proxy in front reported it. Used raw only for the
/// page-view forward, where the backend turns it into a location and drops it.
fn extract_ip(headers: &HeaderMap) -> Option<String> {
    // Try various headers for the real IP
    headers
        .get("x-forwarded-for")
        .and_then(|h| h.to_str().ok())
        .and_then(|s| s.split(',').next())
        .map(|s| s.trim().to_string())
        .or_else(|| {
            headers
                .get("x-real-ip")
                .and_then(|h| h.to_str().ok())
                .map(|s| s.to_string())
        })
        .or_else(|| {
            headers
                .get("cf-connecting-ip")
                .and_then(|h| h.to_str().ok())
                .map(|s| s.to_string())
        })
}

fn hash_ip(ip: &str) -> String {
    // Hash the IP for privacy
    let mut hasher = Sha256::new();
    hasher.update(ip.as_bytes());
    let result = hasher.finalize();
    format!("{:x}", result)[..16].to_string() // Take first 16 chars
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn identify_param_keeps_query_and_fragment() {
        assert_eq!(
            with_identify_param("https://x.com/p", "abc"),
            "https://x.com/p?wbly_t=abc"
        );
        assert_eq!(
            with_identify_param("https://x.com/p?a=1#top", "abc"),
            "https://x.com/p?a=1&wbly_t=abc#top"
        );
    }

    #[test]
    fn host_of_strips_scheme_and_path() {
        assert_eq!(host_of("https://WWW.Example.com/a?b#c"), "www.example.com");
        assert_eq!(host_of("example.com"), "example.com");
    }
}
