package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (h *accountHandler) applySeller(c *gin.Context) {
	var input struct {
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_REQUEST", "请提交包含 description 的 JSON")
		return
	}
	application, err := h.service.ApplySeller(c.Request.Context(), c.MustGet(currentUserKey).(int64), input.Description)
	if err != nil {
		h.failed(c, err)
		return
	}
	c.JSON(http.StatusCreated, response{Data: application})
}

func (h *accountHandler) myApplications(c *gin.Context) {
	applications, err := h.service.MyApplications(c.Request.Context(), c.MustGet(currentUserKey).(int64))
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, applications)
}

func (h *accountHandler) adminApplications(c *gin.Context) {
	applications, err := h.service.AdminApplications(c.Request.Context(), c.MustGet(currentUserKey).(int64))
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, applications)
}

func (h *accountHandler) reviewApplication(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 32)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, "INVALID_APPLICATION_ID", "申请编号必须是有效的正整数")
		return
	}
	var input struct {
		Status       string `json:"status"`
		ReviewReason string `json:"review_reason"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_REQUEST", "请提交 status，拒绝时还需 review_reason")
		return
	}
	application, err := h.service.ReviewApplication(c.Request.Context(), c.MustGet(currentUserKey).(int64), id, input.Status, input.ReviewReason)
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, application)
}
