package catalog

import (
	"context"
	_ "embed"
	"fmt"

	"gorm.io/gorm"
)

//go:embed shops.sql
var shopsSQL string

//go:embed categories.sql
var categoriesSQL string

//go:embed products.sql
var productsSQL string

//go:embed legacy-products.sql
var legacyProductsSQL string

func EnsureSchema(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	for _, sql := range []string{shopsSQL, categoriesSQL, productsSQL} {
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("创建店铺、分类或商品表: %w", err)
		}
	}
	if !db.Migrator().HasColumn(&Product{}, "shop_id") {
		// 第一轮的商品暂时保留空归属；认领到示例店铺后再收紧 NOT NULL。
		if err := db.Exec(legacyProductsSQL).Error; err != nil {
			return fmt.Errorf("补充第一轮商品字段: %w", err)
		}
	}
	return nil
}
