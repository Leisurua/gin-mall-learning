package httpapi

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"time"

	"gin-mall/internal/account"
	"github.com/gin-gonic/gin"
)

const (
	sessionCookieName = "gin_mall_session"
	currentUserKey    = "current_user_id"
)

type accountHandler struct {
	service      *account.Service
	logger       *slog.Logger
	cookieSecure bool
}

func (h *accountHandler) register(c *gin.Context) {
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_REQUEST", "请提交包含 username、email、password 的 JSON")
		return
	}
	profile, err := h.service.Register(c.Request.Context(), input.Username, input.Email, input.Password)
	if err != nil {
		h.failed(c, err)
		return
	}
	c.JSON(http.StatusCreated, response{Data: profile})
}

func (h *accountHandler) login(c *gin.Context) {
	var input struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_REQUEST", "请提交包含 identifier、password 的 JSON")
		return
	}
	profile, session, err := h.service.Login(c.Request.Context(), input.Identifier, input.Password)
	if err != nil {
		h.failed(c, err)
		return
	}
	h.setCookie(c, session.Token, session.ExpiresAt)
	success(c, profile)
}

func (h *accountHandler) requireLogin(c *gin.Context) {
	token, err := c.Cookie(sessionCookieName)
	if err != nil {
		h.failed(c, account.ErrNotAuthenticated)
		return
	}
	session, err := h.service.Authenticate(c.Request.Context(), token)
	if err != nil {
		if errors.Is(err, account.ErrNotAuthenticated) {
			h.setCookie(c, "", time.Unix(1, 0))
		}
		h.failed(c, err)
		return
	}
	// 编号来自经过验证的 Session，后续处理从 Context 读取。
	c.Set(currentUserKey, session.UserID)
	h.setCookie(c, token, session.ExpiresAt)
	c.Next()
}

func (h *accountHandler) me(c *gin.Context) {
	userID := c.MustGet(currentUserKey).(int64)
	profile, err := h.service.Profile(c.Request.Context(), userID)
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, profile)
}

func (h *accountHandler) setCookie(c *gin.Context, token string, expires time.Time) {
	maxAge := int(math.Ceil(time.Until(expires).Seconds()))
	if maxAge <= 0 {
		maxAge = -1
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/api",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.cookieSecure,
		Expires: expires, MaxAge: maxAge,
	})
}

func (h *accountHandler) failed(c *gin.Context, err error) {
	var validation *account.ValidationError
	switch {
	case errors.As(err, &validation):
		failure(c, http.StatusBadRequest, "INVALID_INPUT", validation.Message)
	case errors.Is(err, account.ErrDuplicateAccount):
		failure(c, http.StatusConflict, "ACCOUNT_EXISTS", account.ErrDuplicateAccount.Error())
	case errors.Is(err, account.ErrForbidden):
		failure(c, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, account.ErrApplicationPending):
		failure(c, http.StatusConflict, "APPLICATION_PENDING", err.Error())
	case errors.Is(err, account.ErrAlreadySeller):
		failure(c, http.StatusConflict, "ALREADY_SELLER", err.Error())
	case errors.Is(err, account.ErrApplicationReviewed):
		failure(c, http.StatusConflict, "APPLICATION_ALREADY_REVIEWED", err.Error())
	case errors.Is(err, account.ErrApplicationNotFound):
		failure(c, http.StatusNotFound, "APPLICATION_NOT_FOUND", err.Error())
	case errors.Is(err, account.ErrInvalidCredentials):
		failure(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", account.ErrInvalidCredentials.Error())
	case errors.Is(err, account.ErrNotAuthenticated):
		failure(c, http.StatusUnauthorized, "NOT_AUTHENTICATED", account.ErrNotAuthenticated.Error())
	case errors.Is(err, account.ErrSessionUnavailable):
		h.logger.Error("Session 操作失败", "error", err)
		failure(c, http.StatusServiceUnavailable, "SESSION_UNAVAILABLE", account.ErrSessionUnavailable.Error())
	default:
		h.logger.Error("账号操作失败", "path", c.Request.URL.Path, "error", err)
		failure(c, http.StatusInternalServerError, "INTERNAL_ERROR", "账号操作失败")
	}
}
