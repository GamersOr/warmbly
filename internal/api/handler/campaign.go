package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/tasks"
)

// templatePreviewRequest is the composer's preview/validate payload: the raw
// templates plus an optional sample contact to render against.
type templatePreviewRequest struct {
	Subject   string `json:"subject"`
	BodyHTML  string `json:"body_html"`
	BodyPlain string `json:"body_plain"`
	Contact   *struct {
		FirstName    string            `json:"first_name"`
		LastName     string            `json:"last_name"`
		Email        string            `json:"email"`
		Company      string            `json:"company"`
		Phone        string            `json:"phone"`
		CustomFields map[string]string `json:"custom_fields"`
	} `json:"contact"`
}

func sampleContact() models.Contact {
	return models.Contact{
		FirstName:    "Alex",
		LastName:     "Rivera",
		Email:        "alex.rivera@example.com",
		Company:      "Acme Inc",
		Phone:        "+1 555 0142",
		CustomFields: map[string]string{"role": "Head of Growth"},
	}
}

// PreviewCampaignTemplate renders subject/body against a sample (or supplied)
// contact EXACTLY as the send path would, returning the output plus any parse
// errors and unresolved tokens. No side effects — powers the composer's live
// preview + inline validation.
func (h *Handler) PreviewCampaignTemplate(c *gin.Context) {
	var req templatePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	contact := sampleContact()
	if rc := req.Contact; rc != nil {
		if rc.FirstName != "" {
			contact.FirstName = rc.FirstName
		}
		if rc.LastName != "" {
			contact.LastName = rc.LastName
		}
		if rc.Email != "" {
			contact.Email = rc.Email
		}
		if rc.Company != "" {
			contact.Company = rc.Company
		}
		if rc.Phone != "" {
			contact.Phone = rc.Phone
		}
		for k, v := range rc.CustomFields {
			contact.CustomFields[k] = v
		}
	}
	c.JSON(http.StatusOK, tasks.PreviewTemplates(req.Subject, req.BodyHTML, req.BodyPlain, contact))
}

func (h *Handler) CreateCampaign(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		// A campaign with no workspace cannot be suppression-checked or
		// entitlement-checked at send time, so it is never created.
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}

	var data models.CreateCampaign

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	resp, err := h.CampaignService.Create(c.Request.Context(), userIDStr, orgID, &data)
	if err != nil {
		errx.JSON(c, err)
		return
	}

	// Audit log
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityCampaign, &resp.ID, nil, map[string]string{"name": resp.Name})

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetCampaign(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	id := c.Param("id")

	resp, err := h.CampaignService.Get(c.Request.Context(), orgID.String(), id)
	if err != nil {
		errx.JSON(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) SearchCampaigns(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	query := c.Query("q")
	cursor := c.Query("cursor")
	folder := c.Query("folder")
	status := c.Query("status")
	limit := c.Query("limit")

	resp, err := h.CampaignService.Search(c.Request.Context(), orgID.String(), query, cursor, folder, status, limit)
	if err != nil {
		errx.JSON(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetCampaignsOverview returns status-bucket counts and per-folder totals
// for the campaigns browser sidebar.
func (h *Handler) GetCampaignsOverview(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	resp, err := h.CampaignService.Overview(c.Request.Context(), orgID.String())
	if err != nil {
		errx.JSON(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateCampaign(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	id := c.Param("id")

	var data models.UpdateCampaign

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	resp, err := h.CampaignService.Update(c.Request.Context(), userIDStr, id, &data)
	if err != nil {
		errx.JSON(c, err)
		return
	}

	// Audit log
	if campaignID, err := uuid.Parse(id); err == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, nil, nil)
	}

	c.JSON(http.StatusOK, resp)
}

// DeleteCampaign permanently removes a campaign of the current organization.
// DELETE /campaigns/:id
func (h *Handler) DeleteCampaign(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}

	id := c.Param("id")
	campaignID, err := uuid.Parse(id)
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}

	deleted, xerr := h.CampaignService.Delete(c.Request.Context(), *orgID, id)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	// The row is gone, so the name travels in the audit entry.
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityCampaign, &campaignID, nil, map[string]string{"name": deleted.Name})

	c.Status(http.StatusNoContent)
}

// duplicateCampaignRequest is the optional body of a duplicate call.
type duplicateCampaignRequest struct {
	Name string `json:"name"`
}

// DuplicateCampaign creates a draft copy of a campaign's configuration.
// POST /campaigns/:id/duplicate
func (h *Handler) DuplicateCampaign(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.ErrNoOrganization)
		return
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUser)
		return
	}
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}

	// The body is optional; an empty one reads as io.EOF.
	var req duplicateCampaignRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	campaign, xerr := h.CampaignService.Duplicate(c.Request.Context(), *orgID, userID, campaignID.String(), strings.TrimSpace(req.Name))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDuplicate, models.AuditEntityCampaign, &campaign.ID, nil, map[string]string{
		"source": campaignID.String(), "name": campaign.Name,
	})

	c.JSON(http.StatusCreated, campaign)
}

// StartCampaign starts a campaign
// POST /campaigns/:id/start
func (h *Handler) StartCampaign(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	id := c.Param("id")

	// Optional body: {"acknowledge_list_risk": true} launches past the
	// bounce-risk gate once the member has read the projection.
	var opts models.StartCampaignOptions
	if c.Request.ContentLength != 0 {
		_ = c.ShouldBindJSON(&opts)
	}

	if xerr := h.CampaignService.StartCampaign(c.Request.Context(), *orgID, id, opts); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	if campaignID, err := uuid.Parse(id); err == nil {
		h.auditOrg(c, models.AuditActionStart, models.AuditEntityCampaign, &campaignID, nil, nil)
	}

	c.JSON(http.StatusOK, gin.H{"status": "started"})
}

// StopCampaign stops a campaign
// POST /campaigns/:id/stop
func (h *Handler) StopCampaign(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	id := c.Param("id")

	if xerr := h.CampaignService.StopCampaign(c.Request.Context(), *orgID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	if campaignID, err := uuid.Parse(id); err == nil {
		h.auditOrg(c, models.AuditActionStop, models.AuditEntityCampaign, &campaignID, nil, nil)
	}

	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// GetCampaignLogs returns campaign activity logs
// GET /campaigns/:id/logs
func (h *Handler) GetCampaignLogs(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id := c.Param("id")

	cursorStr := c.Query("cursor")
	var cursor *string
	if cursorStr != "" {
		cursor = &cursorStr
	}

	limit := 50
	if limitStr := c.DefaultQuery("limit", "50"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	result, xerr := h.CampaignService.GetLogs(c.Request.Context(), userID, id, limit, cursor)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// ListCampaignSenders returns a campaign's explicit sender pool.
// GET /campaigns/:id/senders
func (h *Handler) ListCampaignSenders(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	senders, xerr := h.CampaignService.ListCampaignSenders(c.Request.Context(), *orgID, c.Param("id"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": senders})
}

// ReplaceCampaignSenders atomically replaces a campaign's explicit sender pool.
// PUT /campaigns/:id/senders
func (h *Handler) ReplaceCampaignSenders(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var body struct {
		Senders []models.CampaignSenderInput `json:"senders"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	senders, xerr := h.CampaignService.ReplaceCampaignSenders(c.Request.Context(), *orgID, c.Param("id"), body.Senders)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	if campaignID, err := uuid.Parse(c.Param("id")); err == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, nil, map[string]string{"scope": "senders"})
	}

	c.JSON(http.StatusOK, gin.H{"data": senders})
}

// VerifyCampaignTrackingDomain resolves the campaign-scoped tracking domain's
// CNAME and flips verified on success.
// POST /campaigns/:id/tracking-domain/verify
func (h *Handler) VerifyCampaignTrackingDomain(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	status, xerr := h.CampaignService.VerifyCampaignTrackingDomain(c.Request.Context(), *orgID, c.Param("id"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	if campaignID, err := uuid.Parse(c.Param("id")); err == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, nil, map[string]string{"scope": "tracking_domain_verify"})
	}

	c.JSON(http.StatusOK, status)
}
