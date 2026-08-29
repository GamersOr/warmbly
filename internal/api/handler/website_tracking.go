package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/websitetracking"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Website tracking settings (JWT only, MANAGE_SETTINGS): the snippet's site
// key, consent mode, location precision, allowed hosts and retention.

func (h *Handler) GetWebsiteTrackingSettings(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	settings, xerr := h.WebsiteTrackingService.GetSettings(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, settings)
}

func (h *Handler) UpdateWebsiteTrackingSettings(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user id"))
		return
	}
	var req models.UpdateWebsiteTrackingSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	settings, xerr := h.WebsiteTrackingService.UpdateSettings(c.Request.Context(), *orgID, userID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySettings, nil, nil, map[string]string{
		"scope": "website_tracking",
	})
	c.JSON(http.StatusOK, settings)
}

// RotateWebsiteTrackingKey issues a new site key. Snippets carrying the old
// one are refused from the next hit on.
func (h *Handler) RotateWebsiteTrackingKey(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user id"))
		return
	}
	settings, xerr := h.WebsiteTrackingService.RotateSiteKey(c.Request.Context(), *orgID, userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySettings, nil, nil, map[string]string{
		"scope":  "website_tracking",
		"action": "rotate_site_key",
	})
	c.JSON(http.StatusOK, settings)
}

// InternalIngestPageHit is where the tracking service forwards a page view
// after its own rate limiting, filtering and payload caps.
//
//	POST /api/v1/internal/page-hits
//	  -> 204 accepted or quietly rejected | 200 {"new_visitor_key":...}
//	  -> 400 malformed | 404 unknown site key
//
// Unknown keys 404 so the edge can cache them negatively and cut off a
// probing source, exactly like an unknown click ticket.
func (h *Handler) InternalIngestPageHit(c *gin.Context) {
	var req models.WebsiteHitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	result, err := h.WebsiteTrackingService.Ingest(c.Request.Context(), &req)
	switch {
	case errors.Is(err, websitetracking.ErrMalformed):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hit"})
	case errors.Is(err, websitetracking.ErrUnknownSite):
		c.Status(http.StatusNotFound)
	case errors.Is(err, websitetracking.ErrRejected):
		c.Status(http.StatusNoContent)
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ingest failed"})
	case result != nil && result.NewVisitorKey != "":
		c.JSON(http.StatusOK, result)
	default:
		c.Status(http.StatusNoContent)
	}
}

// websiteIdentifyForLink is the click redirect's question: may this
// destination carry the visitor-identification ticket?
func (h *Handler) websiteIdentifyForLink(c *gin.Context, campaignID uuid.UUID, destination string) bool {
	if h.WebsiteTrackingService == nil {
		return false
	}
	return h.WebsiteTrackingService.ShouldIdentify(c.Request.Context(), campaignID, destination)
}
