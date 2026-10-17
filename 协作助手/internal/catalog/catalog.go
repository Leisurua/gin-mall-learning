package catalog

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	OffSale = "off_sale"
	OnSale  = "on_sale"
)

var (
	ErrForbidden       = errors.New("只有卖家可以管理自己的店铺和商品")
	ErrShopExists      = errors.New("一个卖家只能创建一家店铺")
	ErrShopNotFound    = errors.New("请先创建店铺")
	ErrProductNotFound = errors.New("商品不存在或不属于你的店铺")
	ErrNoStock         = errors.New("库存为 0，补充库存后才能上架")
	pricePattern       = regexp.MustCompile(`^(0|[1-9][0-9]{0,7})(\.[0-9]{1,2})?$`)
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

type Shop struct {
	ID          int64     `json:"id"`
	OwnerID     int64     `json:"owner_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Category struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Product struct {
	ID          int64     `json:"id"`
	ShopID      int64     `json:"shop_id"`
	CategoryID  int64     `json:"category_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       string    `json:"price"`
	Stock       int64     `json:"stock"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	SoldOut     bool      `json:"sold_out" gorm:"-"`
	Purchasable bool      `json:"purchasable" gorm:"-"`
	SaleMessage string    `json:"sale_message" gorm:"-"`
}

func (p *Product) availability() {
	p.SoldOut = p.Stock == 0
	p.Purchasable = p.Status == OnSale && !p.SoldOut
	switch {
	case p.Status == OffSale:
		p.SaleMessage = "已下架"
	case p.SoldOut:
		p.SaleMessage = "售罄"
	default:
		p.SaleMessage = "在售"
	}
}

// Patch 的指针区分“不修改”和“改成零值”，例如把库存改成 0。
type ProductPatch struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Price       *string `json:"price"`
	Stock       *int64  `json:"stock"`
	CategoryID  *int64  `json:"category_id"`
}

type Filter struct {
	Page, PageSize int
	CategoryID     int64
	Keyword        string
}

type Page struct {
	Items    []Product `json:"items"`
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func requireSeller(db *gorm.DB, userID int64) error {
	var role string
	if err := db.Table("users").Where("id = ?", userID).Pluck("role", &role).Error; err != nil {
		return err
	}
	if role != "seller" {
		return ErrForbidden
	}
	return nil
}

func ownShop(db *gorm.DB, userID int64) (Shop, error) {
	if err := requireSeller(db, userID); err != nil {
		return Shop{}, err
	}
	var shop Shop
	err := db.Where("owner_id = ?", userID).Take(&shop).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return shop, ErrShopNotFound
	}
	return shop, err
}

func (s *Service) CreateShop(ctx context.Context, userID int64, name, description string) (Shop, error) {
	db := s.db.WithContext(ctx)
	if err := requireSeller(db, userID); err != nil {
		return Shop{}, err
	}
	name = strings.TrimSpace(name)
	if err := validateText(name, description); err != nil {
		return Shop{}, err
	}
	now := time.Now().UTC()
	shop := Shop{OwnerID: userID, Name: name, Description: description, CreatedAt: now, UpdatedAt: now}
	err := db.Create(&shop).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return Shop{}, ErrShopExists
	}
	return shop, err
}

func (s *Service) MyShop(ctx context.Context, userID int64) (Shop, error) {
	return ownShop(s.db.WithContext(ctx), userID)
}

func (s *Service) Categories(ctx context.Context) ([]Category, error) {
	items := make([]Category, 0)
	err := s.db.WithContext(ctx).Order("id").Find(&items).Error
	return items, err
}

func validateText(name, description string) error {
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 255 {
		return &ValidationError{"名称不能为空，最多 255 个字符"}
	}
	if !utf8.ValidString(description) || len(description) > 65535 {
		return &ValidationError{"说明最多 65535 个 UTF-8 字节"}
	}
	return nil
}

func normalizePrice(price string) (string, error) {
	if !pricePattern.MatchString(price) {
		return "", &ValidationError{"价格须是大于 0、最多两位小数的金额字符串，最大为 99999999.99"}
	}
	whole, fraction, _ := strings.Cut(price, ".")
	fraction += strings.Repeat("0", 2-len(fraction))
	if whole == "0" && fraction == "00" {
		return "", &ValidationError{"价格必须大于 0"}
	}
	return whole + "." + fraction, nil
}

func validateProduct(db *gorm.DB, p *Product) error {
	p.Name = strings.TrimSpace(p.Name)
	if err := validateText(p.Name, p.Description); err != nil {
		return err
	}
	var err error
	p.Price, err = normalizePrice(p.Price)
	if err != nil {
		return err
	}
	if p.Stock < 0 || p.Stock > math.MaxInt32 {
		return &ValidationError{"库存须为 0 到 2147483647 之间的整数"}
	}
	if p.CategoryID <= 0 || p.CategoryID > math.MaxInt32 {
		return &ValidationError{"请选择有效的分类编号"}
	}
	var count int64
	if err := db.Model(&Category{}).Where("id = ?", p.CategoryID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return &ValidationError{"分类不存在，请从平台分类目录中选择"}
	}
	return nil
}

func (s *Service) CreateProduct(ctx context.Context, userID int64, p Product) (Product, error) {
	db := s.db.WithContext(ctx)
	shop, err := ownShop(db, userID)
	if err != nil {
		return Product{}, err
	}
	// 归属、状态和时间由服务端决定，不接受客户端指定。
	p.ID, p.ShopID, p.Status = 0, shop.ID, OffSale
	if err := validateProduct(db, &p); err != nil {
		return Product{}, err
	}
	p.CreatedAt, p.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	err = db.Create(&p).Error
	p.availability()
	return p, err
}

func (s *Service) Product(ctx context.Context, id int64) (Product, error) {
	var p Product
	err := s.db.WithContext(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return p, ErrProductNotFound
	}
	p.availability()
	return p, err
}

func (s *Service) OwnProduct(ctx context.Context, userID, id int64) (Product, error) {
	shop, err := ownShop(s.db.WithContext(ctx), userID)
	if err != nil {
		return Product{}, err
	}
	return findOwnProduct(s.db.WithContext(ctx), shop.ID, id, false)
}

func findOwnProduct(db *gorm.DB, shopID, id int64, lock bool) (Product, error) {
	if lock {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var p Product
	err := db.Where("shop_id = ? AND id = ?", shopID, id).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return p, ErrProductNotFound
	}
	p.availability()
	return p, err
}

func (s *Service) UpdateProduct(ctx context.Context, userID, id int64, patch ProductPatch) (Product, error) {
	return s.changeProduct(ctx, userID, id, func(tx *gorm.DB, p *Product) error {
		if patch.Name == nil && patch.Description == nil && patch.Price == nil && patch.Stock == nil && patch.CategoryID == nil {
			return &ValidationError{"请至少提交一个需要修改的商品字段"}
		}
		if patch.Name != nil {
			p.Name = *patch.Name
		}
		if patch.Description != nil {
			p.Description = *patch.Description
		}
		if patch.Price != nil {
			p.Price = *patch.Price
		}
		if patch.Stock != nil {
			p.Stock = *patch.Stock
		}
		if patch.CategoryID != nil {
			p.CategoryID = *patch.CategoryID
		}
		// 已在售商品的库存可改为 0，状态不变；响应中派生出售罄标记。
		return validateProduct(tx, p)
	})
}

func (s *Service) ChangeStatus(ctx context.Context, userID, id int64, status string) (Product, error) {
	return s.changeProduct(ctx, userID, id, func(_ *gorm.DB, p *Product) error {
		if status != OnSale && status != OffSale {
			return &ValidationError{"状态只能是 on_sale 或 off_sale"}
		}
		if status == OnSale && p.Status != OnSale && p.Stock == 0 {
			return ErrNoStock
		}
		p.Status = status
		return nil
	})
}

func (s *Service) changeProduct(ctx context.Context, userID, id int64, change func(*gorm.DB, *Product) error) (Product, error) {
	var p Product
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		shop, err := ownShop(tx, userID)
		if err != nil {
			return err
		}
		p, err = findOwnProduct(tx, shop.ID, id, true)
		if err != nil {
			return err
		}
		if err := change(tx, &p); err != nil {
			return err
		}
		p.UpdatedAt = time.Now().UTC()
		// map 能保存 0 库存和空说明，且更新条件再次限制为自己的店铺。
		return tx.Model(&Product{}).Where("id = ? AND shop_id = ?", p.ID, shop.ID).Updates(map[string]any{
			"name": p.Name, "description": p.Description, "price": p.Price, "stock": p.Stock,
			"category_id": p.CategoryID, "status": p.Status, "updated_at": p.UpdatedAt,
		}).Error
	})
	p.availability()
	return p, err
}

func (s *Service) Products(ctx context.Context, f Filter, ownerID *int64) (Page, error) {
	result := Page{Items: make([]Product, 0), Page: f.Page, PageSize: f.PageSize}
	if f.Page < 1 || f.PageSize < 1 || f.PageSize > 100 || f.Page > math.MaxInt/f.PageSize {
		return result, &ValidationError{"page 须为正整数，page_size 须为 1 到 100；页码不能溢出"}
	}
	query := s.db.WithContext(ctx).Model(&Product{})
	if ownerID == nil {
		query = query.Where("status = ?", OnSale)
	} else {
		shop, err := ownShop(s.db.WithContext(ctx), *ownerID)
		if err != nil {
			return result, err
		}
		query = query.Where("shop_id = ?", shop.ID)
	}
	if f.CategoryID != 0 {
		query = query.Where("category_id = ?", f.CategoryID)
	}
	if f.Keyword != "" {
		// 将 % 和 _ 当成用户输入的普通字符，而非 LIKE 的通配符。
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Keyword)
		query = query.Where("(name LIKE ? ESCAPE '!' OR description LIKE ? ESCAPE '!')", "%"+escaped+"%", "%"+escaped+"%")
	}
	if err := query.Count(&result.Total).Error; err != nil {
		return result, err
	}
	err := query.Order("created_at DESC, id DESC").Limit(f.PageSize).Offset((f.Page - 1) * f.PageSize).Find(&result.Items).Error
	for i := range result.Items {
		result.Items[i].availability()
	}
	return result, err
}
