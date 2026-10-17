package httpapi

import (
	"errors"
	"log/slog"
	"math"
	"net/http"

	"gin-mall/internal/cart"
	"github.com/gin-gonic/gin"
)

type cartHandler struct {
	service *cart.Service
	logger  *slog.Logger
}

func (h *cartHandler) list(c *gin.Context) {
	items, err := h.service.List(c.Request.Context(), c.MustGet(currentUserKey).(int64))
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, gin.H{"items": items})
}

func (h *cartHandler) add(c *gin.Context) {
	var input struct {
		ProductID *int64 `json:"product_id"`
		Quantity  *int64 `json:"quantity"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.ProductID == nil || *input.ProductID <= 0 ||
		*input.ProductID > math.MaxInt32 || input.Quantity == nil || *input.Quantity < 0 {
		failure(c, http.StatusBadRequest, "INVALID_INPUT", "请提交有效的商品编号 product_id 和非负整数 quantity")
		return
	}
	result, err := h.service.Add(c.Request.Context(), c.MustGet(currentUserKey).(int64), *input.ProductID, *input.Quantity)
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, result)
}

func (h *cartHandler) update(c *gin.Context) {
	id, ok := productID(c)
	if !ok {
		return
	}
	var input struct {
		Quantity *int64 `json:"quantity"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Quantity == nil || *input.Quantity < 0 {
		failure(c, http.StatusBadRequest, "INVALID_INPUT", "请提交非负整数 quantity，0 表示移除；负数、小数或缺少数量均不合法")
		return
	}
	result, err := h.service.SetQuantity(c.Request.Context(), c.MustGet(currentUserKey).(int64), id, *input.Quantity)
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, result)
}

func (h *cartHandler) remove(c *gin.Context) {
	id, ok := productID(c)
	if !ok {
		return
	}
	result, err := h.service.Remove(c.Request.Context(), c.MustGet(currentUserKey).(int64), id)
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, result)
}

func (h *cartHandler) failed(c *gin.Context, err error) {
	var validation *cart.ValidationError
	var conflict *cart.ConflictError
	switch {
	case errors.As(err, &validation):
		failure(c, http.StatusBadRequest, "INVALID_INPUT", validation.Message)
	case errors.As(err, &conflict):
		failure(c, http.StatusConflict, conflict.Code, conflict.Message)
	case errors.Is(err, cart.ErrItemNotFound):
		failure(c, http.StatusNotFound, "CART_ITEM_NOT_FOUND", err.Error())
	case errors.Is(err, cart.ErrProductNotFound):
		failure(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", err.Error())
	default:
		h.logger.Error("购物车操作失败", "path", c.Request.URL.Path, "error", err)
		failure(c, http.StatusInternalServerError, "INTERNAL_ERROR", "购物车操作失败")
	}
}
