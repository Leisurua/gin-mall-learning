package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Product struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Price string `json:"price"`
}

type productHandler struct {
	db     *gorm.DB
	logger *slog.Logger
}

func (h *productHandler) list(c *gin.Context) {
	// 空列表返回 []，而不是 null。
	products := make([]Product, 0)
	if err := h.db.WithContext(c.Request.Context()).Order("id").Find(&products).Error; err != nil {
		h.queryFailed(c, err)
		return
	}
	success(c, products)
}

func (h *productHandler) detail(c *gin.Context) {
	// products.id 是 MySQL INT，取值限制为正的 32 位整数。
	id, err := strconv.ParseInt(c.Param("id"), 10, 32)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, "INVALID_PRODUCT_ID",
			"商品编号必须是 1 到 2147483647 之间的整数")
		return
	}

	var product Product
	err = h.db.WithContext(c.Request.Context()).First(&product, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		failure(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "商品不存在")
		return
	}
	if err != nil {
		h.queryFailed(c, err)
		return
	}
	success(c, product)
}

func (h *productHandler) queryFailed(c *gin.Context, err error) {
	h.logger.Error("商品查询失败", "path", c.Request.URL.Path, "error", err)
	failure(c, http.StatusInternalServerError, "INTERNAL_ERROR", "商品查询失败")
}
