package cart

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"gin-mall/internal/catalog"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrItemNotFound    = errors.New("你的购物车中没有此商品")
	ErrProductNotFound = errors.New("商品不存在")
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

type ConflictError struct{ Code, Message string }

func (e *ConflictError) Error() string { return e.Message }

type Item struct {
	UserID    int64     `json:"-" gorm:"primaryKey;autoIncrement:false"`
	ProductID int64     `json:"product_id" gorm:"primaryKey;autoIncrement:false"`
	Quantity  int64     `json:"quantity"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Item) TableName() string { return "cart_items" }

type ItemView struct {
	Item
	Product              catalog.Product `json:"product"`
	QuantityExceedsStock bool            `json:"quantity_exceeds_stock"`
	CanChangeQuantity    bool            `json:"can_change_quantity"`
	Message              string          `json:"message"`
}

type Result struct {
	ProductID int64     `json:"product_id"`
	Quantity  int64     `json:"quantity"`
	Changed   bool      `json:"changed"`
	Adjusted  bool      `json:"adjusted"`
	Removed   bool      `json:"removed"`
	Message   string    `json:"message"`
	Item      *ItemView `json:"item"`
}

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

//go:embed items.sql
var itemsSQL string

func EnsureSchema(ctx context.Context, db *gorm.DB) error {
	if err := db.WithContext(ctx).Exec(itemsSQL).Error; err != nil {
		return fmt.Errorf("创建购物项表: %w", err)
	}
	return nil
}

func view(item Item, product catalog.Product) ItemView {
	product.RefreshAvailability()
	v := ItemView{Item: item, Product: product,
		QuantityExceedsStock: item.Quantity > product.Stock,
		CanChangeQuantity:    product.Purchasable}
	switch {
	case !product.Purchasable:
		v.Message = product.SaleMessage
	case v.QuantityExceedsStock:
		v.Message = fmt.Sprintf("库存不足，当前仅剩 %d 件，请修改数量", product.Stock)
	}
	return v
}

func (s *Service) List(ctx context.Context, userID int64) ([]ItemView, error) {
	db := s.db.WithContext(ctx)
	var items []Item
	if err := db.Where("user_id = ?", userID).Order("product_id").Find(&items).Error; err != nil {
		return nil, err
	}
	result := make([]ItemView, 0, len(items))
	if len(items) == 0 {
		return result, nil
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ProductID)
	}
	// 批量读取最新商品信息；查询不更新购物项数量或时间。
	var products []catalog.Product
	if err := db.Where("id IN ?", ids).Find(&products).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]catalog.Product, len(products))
	for _, p := range products {
		byID[p.ID] = p
	}
	for _, item := range items {
		p, ok := byID[item.ProductID]
		if !ok {
			return nil, fmt.Errorf("购物项关联的商品 %d 不存在", item.ProductID)
		}
		result = append(result, view(item, p))
	}
	return result, nil
}

func (s *Service) Add(ctx context.Context, userID, productID, quantity int64) (Result, error) {
	if quantity < 0 {
		return Result{}, &ValidationError{"加入数量须为非负整数"}
	}
	if quantity == 0 {
		// 明确提交 0 是成功的空操作，不创建记录，也不更新时间。
		result := Result{ProductID: productID, Message: "加入 0 件，购物车未改变"}
		var item Item
		err := s.db.WithContext(ctx).Where("user_id = ? AND product_id = ?", userID, productID).Take(&item).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return Result{}, err
		}
		result.Quantity = item.Quantity
		return result, nil
	}
	return s.set(ctx, userID, productID, quantity, true)
}

func (s *Service) SetQuantity(ctx context.Context, userID, productID, quantity int64) (Result, error) {
	if quantity < 0 {
		return Result{}, &ValidationError{"修改数量须为非负整数，0 表示移除"}
	}
	if quantity == 0 {
		return s.Remove(ctx, userID, productID)
	}
	return s.set(ctx, userID, productID, quantity, false)
}

func (s *Service) set(ctx context.Context, userID, productID, quantity int64, adding bool) (Result, error) {
	result := Result{ProductID: productID}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 同一用户的检查和写入一起完成，首次加入也不会因尚无购物项而绕开同步。
		var owner struct{ ID int64 }
		if err := tx.Table("users").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).Take(&owner).Error; err != nil {
			return err
		}
		var item Item
		err := tx.Where("user_id = ? AND product_id = ?", userID, productID).Take(&item).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !adding && !exists {
			return ErrItemNotFound
		}
		var product catalog.Product
		if err := tx.First(&product, productID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrProductNotFound
			}
			return err
		}
		product.RefreshAvailability()
		if !product.Purchasable {
			return &ConflictError{"PRODUCT_UNAVAILABLE", product.SaleMessage + "，只能查看或移除已有购物项"}
		}
		if adding && item.Quantity > product.Stock {
			return &ConflictError{"CART_STOCK_EXCEEDED", fmt.Sprintf("库存仅剩 %d 件，无法继续增加；原数量保持不变", product.Stock)}
		}
		actual := quantity
		if adding {
			// 先比较剩余空间，避免相加溢出；此处不扣库存。
			result.Adjusted = quantity > product.Stock-item.Quantity
			if result.Adjusted {
				actual = product.Stock
			} else {
				actual = item.Quantity + quantity
			}
		} else {
			result.Adjusted = quantity > product.Stock
			actual = min(quantity, product.Stock)
		}
		result.Changed = !exists || item.Quantity != actual
		if !exists {
			now := time.Now().UTC()
			item = Item{UserID: userID, ProductID: productID, Quantity: actual, CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		} else if result.Changed {
			item.Quantity, item.UpdatedAt = actual, time.Now().UTC()
			if err := tx.Model(&Item{}).Where("user_id = ? AND product_id = ?", userID, productID).
				Updates(map[string]any{"quantity": item.Quantity, "updated_at": item.UpdatedAt}).Error; err != nil {
				return err
			}
		}
		result.Quantity = item.Quantity
		v := view(item, product)
		result.Item = &v
		result.Message = "购物车已更新"
		if !result.Changed {
			result.Message = "数量未改变"
		}
		if result.Adjusted {
			result.Message = fmt.Sprintf("库存仅剩 %d 件，购物车数量已调整为 %d 件", product.Stock, item.Quantity)
		}
		return nil
	})
	return result, err
}

func (s *Service) Remove(ctx context.Context, userID, productID int64) (Result, error) {
	// 始终用登录用户与商品的组合定位，不接受客户端指定购物项归属。
	deleted := s.db.WithContext(ctx).Where("user_id = ? AND product_id = ?", userID, productID).Delete(&Item{})
	if deleted.Error != nil {
		return Result{}, deleted.Error
	}
	if deleted.RowsAffected == 0 {
		return Result{}, ErrItemNotFound
	}
	return Result{ProductID: productID, Changed: true, Removed: true, Message: "已移除购物项"}, nil
}
