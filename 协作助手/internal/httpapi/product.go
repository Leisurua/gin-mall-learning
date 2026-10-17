package httpapi

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"gin-mall/internal/catalog"
	"github.com/gin-gonic/gin"
)

type productHandler struct {
	service *catalog.Service
	logger  *slog.Logger
}

func (h *productHandler) list(c *gin.Context) { h.products(c, nil) }

func (h *productHandler) ownList(c *gin.Context) {
	userID := c.MustGet(currentUserKey).(int64)
	h.products(c, &userID)
}

func (h *productHandler) products(c *gin.Context, ownerID *int64) {
	filter, err := productFilter(c)
	if err != nil {
		h.failed(c, err)
		return
	}
	page, err := h.service.Products(c.Request.Context(), filter, ownerID)
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, page)
}

func productFilter(c *gin.Context) (catalog.Filter, error) {
	f := catalog.Filter{Page: 1, PageSize: 20, Keyword: c.Query("keyword")}
	for key, target := range map[string]*int{"page": &f.Page, "page_size": &f.PageSize} {
		if values, present := c.Request.URL.Query()[key]; present {
			if len(values) != 1 {
				return f, &catalog.ValidationError{Message: key + " 只能出现一次"}
			}
			number, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil || number < 1 || number > int64(math.MaxInt) {
				return f, &catalog.ValidationError{Message: key + " 须为有效的正整数"}
			}
			*target = int(number)
		}
	}
	if values, present := c.Request.URL.Query()["category_id"]; present {
		if len(values) != 1 {
			return f, &catalog.ValidationError{Message: "category_id 只能出现一次"}
		}
		id, err := strconv.ParseInt(values[0], 10, 32)
		if err != nil || id <= 0 {
			return f, &catalog.ValidationError{Message: "category_id 须为有效的正整数"}
		}
		f.CategoryID = id
	}
	return f, nil
}

func productID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 32)
	if err != nil || id <= 0 {
		failure(c, http.StatusBadRequest, "INVALID_PRODUCT_ID", "商品编号须为 1 到 2147483647 之间的整数")
		return 0, false
	}
	return id, true
}

func (h *productHandler) detail(c *gin.Context) {
	id, ok := productID(c)
	if !ok {
		return
	}
	p, err := h.service.Product(c.Request.Context(), id)
	if errors.Is(err, catalog.ErrProductNotFound) {
		failure(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", "商品不存在")
		return
	}
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, p)
}

func (h *productHandler) ownDetail(c *gin.Context) {
	id, ok := productID(c)
	if !ok {
		return
	}
	p, err := h.service.OwnProduct(c.Request.Context(), c.MustGet(currentUserKey).(int64), id)
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, p)
}

func (h *productHandler) create(c *gin.Context) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Price       string `json:"price"`
		Stock       *int64 `json:"stock"`
		CategoryID  int64  `json:"category_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Stock == nil {
		failure(c, http.StatusBadRequest, "INVALID_REQUEST", "请提交名称、金额字符串、整数库存和分类编号；说明可为空")
		return
	}
	p, err := h.service.CreateProduct(c.Request.Context(), c.MustGet(currentUserKey).(int64), catalog.Product{
		Name: input.Name, Description: input.Description, Price: input.Price, Stock: *input.Stock, CategoryID: input.CategoryID,
	})
	if err != nil {
		h.failed(c, err)
		return
	}
	c.JSON(http.StatusCreated, response{Data: p})
}

func (h *productHandler) update(c *gin.Context) {
	id, ok := productID(c)
	if !ok {
		return
	}
	var patch catalog.ProductPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_REQUEST", "商品修改字段格式有误，金额须为字符串，库存须为整数")
		return
	}
	p, err := h.service.UpdateProduct(c.Request.Context(), c.MustGet(currentUserKey).(int64), id, patch)
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, p)
}

func (h *productHandler) status(c *gin.Context) {
	id, ok := productID(c)
	if !ok {
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_REQUEST", "请提交 status: on_sale 或 off_sale")
		return
	}
	p, err := h.service.ChangeStatus(c.Request.Context(), c.MustGet(currentUserKey).(int64), id, input.Status)
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, p)
}

func (h *productHandler) categories(c *gin.Context) {
	items, err := h.service.Categories(c.Request.Context())
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, items)
}

func (h *productHandler) createShop(c *gin.Context) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_REQUEST", "请提交店铺名称 name 和说明 description")
		return
	}
	shop, err := h.service.CreateShop(c.Request.Context(), c.MustGet(currentUserKey).(int64), input.Name, input.Description)
	if err != nil {
		h.failed(c, err)
		return
	}
	c.JSON(http.StatusCreated, response{Data: shop})
}

func (h *productHandler) myShop(c *gin.Context) {
	shop, err := h.service.MyShop(c.Request.Context(), c.MustGet(currentUserKey).(int64))
	if err != nil {
		h.failed(c, err)
		return
	}
	success(c, shop)
}

func (h *productHandler) failed(c *gin.Context, err error) {
	var validation *catalog.ValidationError
	switch {
	case errors.As(err, &validation):
		failure(c, http.StatusBadRequest, "INVALID_INPUT", validation.Message)
	case errors.Is(err, catalog.ErrForbidden):
		failure(c, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, catalog.ErrShopExists):
		failure(c, http.StatusConflict, "SHOP_EXISTS", err.Error())
	case errors.Is(err, catalog.ErrShopNotFound):
		failure(c, http.StatusNotFound, "SHOP_NOT_FOUND", err.Error())
	case errors.Is(err, catalog.ErrProductNotFound):
		failure(c, http.StatusNotFound, "PRODUCT_NOT_FOUND", err.Error())
	case errors.Is(err, catalog.ErrNoStock):
		failure(c, http.StatusConflict, "NO_STOCK", err.Error())
	default:
		h.logger.Error("商品或店铺操作失败", "path", c.Request.URL.Path, "error", err)
		failure(c, http.StatusInternalServerError, "INTERNAL_ERROR", "商品或店铺操作失败")
	}
}
