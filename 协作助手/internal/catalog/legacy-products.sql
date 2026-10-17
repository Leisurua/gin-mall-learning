-- 为第一轮已有的 products 补字段。仅在缺少 shop_id 时执行一次。
-- 原商品先保留空归属并下架；认领后可把两个归属字段改为 NOT NULL。
ALTER TABLE products
    ADD COLUMN shop_id INT NULL,
    ADD COLUMN category_id INT NULL,
    ADD COLUMN description TEXT NOT NULL,
    ADD COLUMN stock INT NOT NULL DEFAULT 0,
    ADD COLUMN status ENUM('off_sale', 'on_sale') NOT NULL DEFAULT 'off_sale',
    ADD COLUMN created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    ADD COLUMN updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    ADD CONSTRAINT fk_products_shop FOREIGN KEY (shop_id) REFERENCES shops(id),
    ADD CONSTRAINT fk_products_category FOREIGN KEY (category_id) REFERENCES categories(id),
    ADD CONSTRAINT ck_products_price CHECK (price > 0),
    ADD CONSTRAINT ck_products_stock CHECK (stock >= 0);
