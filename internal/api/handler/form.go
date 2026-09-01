package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// formScope pulls the org and the :id path param once per handler.
func formScope(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, uuid.Nil, false
	}
	raw := c.Param("id")
	if raw == "" {
		return *orgID, uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid form id"))
		return uuid.Nil, uuid.Nil, false
	}
	return *orgID, id, true
}

// withFormShareURL stamps the hosted page URL onto the response.
func withFormShareURL(f *models.Form) *models.Form {
	f.ShareURL = config.GetFormURL(f.PublicID)
	return f
}

func (h *Handler) ListForms(c *gin.Context) {
	orgID, _, ok := formScope(c)
	if !ok {
		return
	}
	out, xerr := h.FormService.List(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	for i := range out {
		withFormShareURL(&out[i])
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// GetFormsConfig tells the builder what this instance can do, so the UI
// never shows a captcha toggle that cannot work.
func (h *Handler) GetFormsConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"base_url":          config.FormsBaseURL(),
		"captcha_available": config.CaptchaProvider() != "none" && config.TurnstileSiteKey() != "",
	})
}

func (h *Handler) GetForm(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	out, xerr := h.FormService.Get(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, withFormShareURL(out))
}

func (h *Handler) CreateForm(c *gin.Context) {
	orgID, _, ok := formScope(c)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	var createdBy *uuid.UUID
	if uid, err := middleware.GetUserUUID(c); err == nil {
		createdBy = &uid
	}
	out, xerr := h.FormService.Create(c.Request.Context(), orgID, createdBy, in.Name)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityForm, &out.ID, nil, map[string]string{"name": out.Name})
	c.JSON(http.StatusCreated, withFormShareURL(out))
}

func (h *Handler) UpdateForm(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	var in models.FormWrite
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}
	out, xerr := h.FormService.Update(c.Request.Context(), orgID, id, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityForm, &out.ID, nil, map[string]string{"name": out.Name, "status": string(out.Status)})
	c.JSON(http.StatusOK, withFormShareURL(out))
}

func (h *Handler) DeleteForm(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	if xerr := h.FormService.Delete(c.Request.Context(), orgID, id); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityForm, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListFormSubmissions(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			errx.Handle(c, errx.New(errx.BadRequest, "limit must be between 1 and 100"))
			return
		}
		limit = n
	}
	var before *time.Time
	if raw := c.Query("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "before must be an RFC 3339 timestamp"))
			return
		}
		before = &t
	}
	out, hasMore, xerr := h.FormService.ListSubmissions(c.Request.Context(), orgID, id, limit, before)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "has_more": hasMore})
}

func (h *Handler) DeleteFormSubmission(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	subID, err := uuid.Parse(c.Param("sid"))
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid submission id"))
		return
	}
	if xerr := h.FormService.DeleteSubmission(c.Request.Context(), orgID, id, subID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityForm, &id, nil, map[string]string{"submission_id": subID.String()})
	c.Status(http.StatusNoContent)
}
