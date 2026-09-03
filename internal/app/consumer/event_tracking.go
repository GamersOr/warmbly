package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// TrackingConsumer handles tracking events from the Rust tracking service. It
// subscribes on the shared event bus (Kafka or NATS) and decodes with the same
// codec the tracking producer writes (Avro on Kafka, JSON on NATS).
type TrackingConsumer struct {
	bus                  eventbus.EventBus
	codec                codec.Codec
	taskRepo             repository.TaskRepository
	campaignProgressRepo repository.CampaignProgressRepository
	campaignRepo         repository.CampaignRepository
	contactRepo          repository.ContactRepository
	evidence             advanced.EvidenceRecorder
	streamingPublisher   *pubsub.StreamingPublisher
	dedupeRepo           repository.TrackingDedupeRepository
	trackedLinks         repository.TrackedLinkRepository
	linkClicks           repository.LinkClickRepository
	// advancedService fires INSTANT open/click action chains the moment a
	// tracking event lands (the open/click analog of the reply path in
	// ProcessIncomingReply). Best-effort and nil-safe: when unset, opens/clicks
	// are still recorded and routed at the next step boundary by the scheduler.
	advancedService advanced.Service
	topic           string
	group           string
}

// NewTrackingConsumer wires the tracking consumer to the shared event bus.
func NewTrackingConsumer(
	bus eventbus.EventBus,
	cdc codec.Codec,
	topic, group string,
	taskRepo repository.TaskRepository,
	campaignProgressRepo repository.CampaignProgressRepository,
	campaignRepo repository.CampaignRepository,
	contactRepo repository.ContactRepository,
	streamingPublisher *pubsub.StreamingPublisher,
	dedupeRepo repository.TrackingDedupeRepository,
	trackedLinks repository.TrackedLinkRepository,
	linkClicks repository.LinkClickRepository,
	advancedService advanced.Service,
	evidence advanced.EvidenceRecorder,
) (*TrackingConsumer, error) {
	return &TrackingConsumer{
		bus:                  bus,
		codec:                cdc,
		taskRepo:             taskRepo,
		campaignProgressRepo: campaignProgressRepo,
		campaignRepo:         campaignRepo,
		contactRepo:          contactRepo,
		streamingPublisher:   streamingPublisher,
		dedupeRepo:           dedupeRepo,
		trackedLinks:         trackedLinks,
		linkClicks:           linkClicks,
		advancedService:      advancedService,
		evidence:             evidence,
		topic:                topic,
		group:                group,
	}, nil
}

// Start subscribes to the tracking topic and blocks until ctx is cancelled.
func (tc *TrackingConsumer) Start(ctx context.Context) error {
	return tc.bus.Subscribe(ctx, []string{tc.topic}, tc.group, tc.receive)
}

// Close is a no-op: the event bus lifecycle is owned by the consumer main,
// which subscribes both worker-events and tracking on the same bus.
func (tc *TrackingConsumer) Close() {}

// receive decodes a tracking-events bus message and dispatches it.
func (tc *TrackingConsumer) receive(_ context.Context, msg eventbus.Message) error {
	var event events.TrackingEvent
	if err := tc.codec.Deserialize(context.Background(), tc.topic, msg.Payload, &event); err != nil {
		log.Warn().Err(err).Msg("failed to deserialize tracking event")
		return nil // don't fail - skip invalid events
	}
	return tc.HandleTrackingEvent(context.Background(), &event)
}

// HandleTrackingEvent processes a tracking event.
//
// Opens and clicks are classified before they count. The edge already drops
// crawlers and security scanners it can name; here the ones it cannot are
// caught by what they do: a fetch with no browser, a fetch inside the
// machine window after dispatch (nobody reads that fast), and clicks on
// several links of one email within seconds (a gateway walking the message).
// A machine open is still recorded, labelled, because it proves delivery. A
// machine click is logged per link with its reason but never stamps the step
// as clicked, fires no automation, and sends no webhook: "clicked" keeps
// meaning a person.
func (tc *TrackingConsumer) HandleTrackingEvent(ctx context.Context, event *events.TrackingEvent) error {
	// Parse and validate task ID
	taskID, err := uuid.Parse(event.TaskID)
	if err != nil {
		// Invalid task ID, skip
		return nil
	}

	// Calculate URL hash for click event deduplication
	urlHash := ""
	if event.EventType == events.EventTypeEmailClicked && event.OriginalURL != nil && *event.OriginalURL != "" {
		urlHash = hashURL(*event.OriginalURL)
	}

	// Get campaign task to find campaign/contact/sequence IDs
	campaignTask, err := tc.taskRepo.GetCampaignTask(ctx, taskID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", event.TaskID).Msg("failed to get campaign task for tracking event")
		return nil
	}
	if campaignTask == nil || campaignTask.CampaignID == nil || campaignTask.ContactID == nil || campaignTask.SequenceID == nil {
		// Task not found, not a campaign task, or missing its linkage: skip
		return nil
	}
	campaignID, contactID, sequenceID := *campaignTask.CampaignID, *campaignTask.ContactID, *campaignTask.SequenceID

	at := eventTime(event.Timestamp)
	sentAt, err := tc.campaignProgressRepo.GetStepSentAt(ctx, campaignID, contactID, sequenceID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", event.TaskID).Msg("failed to read step dispatch time; classifying by user agent only")
		sentAt = nil
	}

	// Classify. Machine opens (Apple MPP prefetch, UA-less clients, a fetch
	// inside the machine window) still count as delivery signal but are
	// labelled, and must never fire open-triggered automations.
	var machine bool
	var reason string
	switch event.EventType {
	case events.EventTypeEmailOpened:
		machine = isMachineOpen(event.UserAgent) || isInstant(sentAt, at)
	case events.EventTypeEmailClicked:
		machine, reason = classifyClick(event.UserAgent, sentAt, at)
	default:
		// Unknown event type, skip
		return nil
	}

	// Check for duplicate at consumer level (belt and suspenders with Rust service)
	if tc.dedupeRepo != nil {
		processed, err := tc.dedupeRepo.IsProcessed(ctx, taskID, event.EventType, urlHash)
		if err != nil {
			// Log but continue - allow processing on dedupe errors
			log.Warn().Err(err).Str("task_id", event.TaskID).Msg("tracking dedupe check failed")
		} else if processed {
			// A HUMAN engagement after a machine-labelled one upgrades the
			// label (a gateway scanned at delivery; the person acted later).
			// Quiet write only: the event was already counted once, so no
			// automations and no re-publish.
			if machine {
				return nil
			}
			switch event.EventType {
			case events.EventTypeEmailOpened:
				_ = tc.campaignProgressRepo.RecordEmailOpened(ctx, campaignID, contactID, sequenceID, false)
			case events.EventTypeEmailClicked:
				tc.upgradeClick(ctx, campaignTask, event, at)
			}
			return nil
		}
	}

	// Record the event, then fire any INSTANT open/click action chain for the
	// contact's current step the moment the signal lands (the open/click analog of
	// the reply path in ProcessIncomingReply). instantKind maps the tracking event
	// to the matcher's eventKind. Firing happens AFTER the Record* write so the
	// matcher reads the just-stamped opened_at / clicked_at off the progress row.
	var instantKind string
	switch event.EventType {
	case events.EventTypeEmailOpened:
		err = tc.campaignProgressRepo.RecordEmailOpened(ctx, campaignID, contactID, sequenceID, machine)
		if !machine {
			instantKind = "open"
			// A human open proves the mailbox is live; a prefetch proves
			// only that a proxy fetched an image.
			if tc.evidence != nil {
				tc.evidence.RecordEvidence(ctx, contactID, "opened", sequenceID.String(), "")
			}
		}
	case events.EventTypeEmailClicked:
		machine, reason, err = tc.recordClick(ctx, campaignTask, event, at, machine, reason)
		if err == nil && !machine {
			err = tc.campaignProgressRepo.RecordEmailClicked(ctx, campaignID, contactID, sequenceID)
			instantKind = "click"
			if tc.evidence != nil {
				tc.evidence.RecordEvidence(ctx, contactID, "clicked", sequenceID.String(), "")
			}
		}
	}

	if err != nil {
		log.Error().Err(err).Str("task_id", event.TaskID).Str("event_type", string(event.EventType)).Msg("failed to record tracking event")
		return nil
	}

	// INSTANT open/click trigger: best-effort and non-blocking, mirroring the
	// reply path. A failure (or a nil service in a process that doesn't wire it)
	// must never block tracking ingest; the scheduler still routes the matching
	// opened/clicked branch at the next step boundary. Exactly-once per (step,
	// eventKind) is enforced inside FireInstantActions via ClaimInstantFire.
	if tc.advancedService != nil && instantKind != "" {
		tc.advancedService.FireInstantActions(ctx, campaignID, contactID, sequenceID, instantKind)
	}

	// Mark as processed for deduplication
	if tc.dedupeRepo != nil {
		if err := tc.dedupeRepo.MarkProcessed(ctx, taskID, event.EventType, urlHash); err != nil {
			log.Warn().Err(err).Str("task_id", event.TaskID).Msg("failed to mark tracking event as processed")
		}
	}

	if machine {
		log.Debug().Str("task_id", event.TaskID).Str("event_type", string(event.EventType)).Str("reason", reason).Msg("tracking event classified as machine")
	}

	// Publish to Pub/Sub for realtime updates
	tc.publishTrackingEvent(ctx, campaignTask, *event, machine)

	return nil
}

// resolveLink names the clicked link: the minted ticket when the event
// carries one (destination and anchor text as stored at send time), else the
// URL the event reports. nil ticket id means the click log row stands alone.
func (tc *TrackingConsumer) resolveLink(ctx context.Context, event *events.TrackingEvent) (*uuid.UUID, string, string) {
	var destination string
	if event.OriginalURL != nil {
		destination = *event.OriginalURL
	}
	if event.LinkID == nil || tc.trackedLinks == nil {
		return nil, destination, ""
	}
	id, err := uuid.Parse(*event.LinkID)
	if err != nil {
		return nil, destination, ""
	}
	link, err := tc.trackedLinks.GetByID(ctx, id)
	if err != nil || link == nil {
		return nil, destination, ""
	}
	if link.Destination != "" {
		destination = link.Destination
	}
	return &link.ID, destination, link.Label
}

// recordClick logs the click per link and applies the burst rule: a click on
// a second link of the same email from the same source inside the burst
// window turns this click AND the earlier ones into machine clicks. When
// that leaves the step with no human click, the clicked stamp the first
// click already wrote is walked back. Returns the final classification.
func (tc *TrackingConsumer) recordClick(ctx context.Context, task *repository.CampaignTask, event *events.TrackingEvent, at time.Time, machine bool, reason string) (bool, string, error) {
	if tc.linkClicks == nil {
		return machine, reason, nil
	}
	linkID, destination, label := tc.resolveLink(ctx, event)
	if destination == "" {
		return machine, reason, nil
	}
	ipHash := ""
	if event.IPHash != nil {
		ipHash = *event.IPHash
	}
	userAgent := ""
	if event.UserAgent != nil {
		userAgent = *event.UserAgent
	}

	burstSince := at.Add(-time.Duration(config.TrackingClickBurstSeconds) * time.Second)
	burst := false
	if !machine && ipHash != "" {
		n, err := tc.linkClicks.CountRecentOtherLinks(ctx, task.TaskID, ipHash, destination, burstSince)
		if err != nil {
			log.Warn().Err(err).Str("task_id", task.TaskID.String()).Msg("burst check failed; treating click as a person's")
		} else if n > 0 {
			machine, reason, burst = true, repository.LinkClickReasonBurst, true
		}
	}

	if err := tc.linkClicks.Insert(ctx, &repository.LinkClick{
		TrackedLinkID: linkID,
		TaskID:        task.TaskID,
		CampaignID:    *task.CampaignID,
		ContactID:     *task.ContactID,
		SequenceID:    *task.SequenceID,
		Destination:   destination,
		Label:         label,
		UserAgent:     userAgent,
		IPHash:        ipHash,
		Machine:       machine,
		MachineReason: reason,
		ClickedAt:     at,
	}); err != nil {
		return machine, reason, err
	}

	if burst {
		if _, err := tc.linkClicks.MarkBurst(ctx, task.TaskID, ipHash, burstSince); err != nil {
			log.Warn().Err(err).Str("task_id", task.TaskID.String()).Msg("failed to relabel burst clicks")
		}
		if err := tc.campaignProgressRepo.UnrecordEmailClicked(ctx, *task.CampaignID, *task.ContactID, *task.SequenceID); err != nil {
			log.Warn().Err(err).Str("task_id", task.TaskID.String()).Msg("failed to walk back the click stamp after a burst")
		}
	}
	return machine, reason, nil
}

// upgradeClick handles a human click on a link this email was already
// credited for: the step is stamped clicked if only machines had clicked so
// far, and the click is logged once so the timeline shows the person's.
func (tc *TrackingConsumer) upgradeClick(ctx context.Context, task *repository.CampaignTask, event *events.TrackingEvent, at time.Time) {
	_ = tc.campaignProgressRepo.RecordEmailClicked(ctx, *task.CampaignID, *task.ContactID, *task.SequenceID)
	if tc.linkClicks == nil {
		return
	}
	linkID, destination, label := tc.resolveLink(ctx, event)
	if destination == "" {
		return
	}
	if seen, err := tc.linkClicks.HasHumanClickOn(ctx, task.TaskID, destination); err != nil || seen {
		return
	}
	ipHash, userAgent := "", ""
	if event.IPHash != nil {
		ipHash = *event.IPHash
	}
	if event.UserAgent != nil {
		userAgent = *event.UserAgent
	}
	_ = tc.linkClicks.Insert(ctx, &repository.LinkClick{
		TrackedLinkID: linkID,
		TaskID:        task.TaskID,
		CampaignID:    *task.CampaignID,
		ContactID:     *task.ContactID,
		SequenceID:    *task.SequenceID,
		Destination:   destination,
		Label:         label,
		UserAgent:     userAgent,
		IPHash:        ipHash,
		ClickedAt:     at,
	})
}

// publishTrackingEvent publishes the tracking event to Pub/Sub for realtime UI
// updates AND fans an opt-in firehose webhook (campaign.email_opened/clicked).
func (tc *TrackingConsumer) publishTrackingEvent(ctx context.Context, task *repository.CampaignTask, event events.TrackingEvent, machine bool) {
	// Get campaign to find user ID + org
	campaign, err := tc.campaignRepo.GetByID(ctx, *task.CampaignID)
	if err != nil || campaign == nil {
		return
	}

	// Get contact email for display
	var contactEmail string
	if task.ContactID != nil {
		contact, xerr := tc.contactRepo.GetByID(ctx, *task.ContactID)
		if xerr == nil && contact != nil {
			contactEmail = contact.Email
		}
	}

	var linkLabel string
	if event.EventType == events.EventTypeEmailClicked {
		_, _, linkLabel = tc.resolveLink(ctx, &event)
	}

	// Fan an opt-in firehose webhook for the open/click (org-scoped). People
	// only: a prefetch or a gateway walking the links is not engagement.
	if tc.advancedService != nil && campaign.OrganizationID != nil && !machine {
		var whType models.WebhookEventType
		switch event.EventType {
		case events.EventTypeEmailOpened:
			whType = models.WebhookEventCampaignEmailOpened
		case events.EventTypeEmailClicked:
			whType = models.WebhookEventCampaignEmailClicked
		}
		if whType != "" {
			data := map[string]any{
				"campaign_id":   task.CampaignID.String(),
				"contact_id":    task.ContactID.String(),
				"contact_email": contactEmail,
				"sequence_id":   task.SequenceID.String(),
			}
			if event.EventType == events.EventTypeEmailClicked && event.OriginalURL != nil {
				data["url"] = *event.OriginalURL
				if linkLabel != "" {
					data["link_label"] = linkLabel
				}
			}
			tc.advancedService.EmitCampaignEvent(ctx, *campaign.OrganizationID, whType, data)
		}
	}

	if tc.streamingPublisher == nil {
		return
	}

	// Determine event type
	var eventType pubsub.EventType
	switch event.EventType {
	case events.EventTypeEmailOpened:
		eventType = pubsub.EventEmailOpened
	case events.EventTypeEmailClicked:
		eventType = pubsub.EventEmailClicked
	default:
		return
	}

	// Publish tracking event (org-scoped: opens/clicks pulse live for the
	// whole team, not just the campaign owner)
	var orgID string
	if campaign.OrganizationID != nil {
		orgID = campaign.OrganizationID.String()
	}
	trackingPayload := &pubsub.TrackingEventPayload{
		BaseEvent: pubsub.BaseEvent{
			EventType: eventType,
			UserID:    campaign.UserID,
			Timestamp: time.Now(),
		},
		OrgID:        orgID,
		CampaignID:   task.CampaignID.String(),
		ContactID:    task.ContactID.String(),
		ContactEmail: contactEmail,
		SequenceID:   task.SequenceID.String(),
		Machine:      machine,
	}

	if event.EventType == events.EventTypeEmailClicked && event.OriginalURL != nil {
		trackingPayload.OriginalURL = *event.OriginalURL
		trackingPayload.LinkLabel = linkLabel
	}

	tc.streamingPublisher.PublishTrackingEvent(ctx, trackingPayload)
}

// hashURL creates a short hash of a URL for deduplication
func hashURL(u string) string {
	if u == "" {
		return ""
	}
	h := sha256.Sum256([]byte(u))
	return hex.EncodeToString(h[:8])
}
