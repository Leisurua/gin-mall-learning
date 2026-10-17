package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"gin-mall/internal/account"
	"gin-mall/internal/cart"
	"gin-mall/internal/catalog"
	"gin-mall/internal/config"
	"gin-mall/internal/infra"
	"gopkg.in/yaml.v3"
)

// 显式指定本机配置才运行。只在新建的独立数据库中检查，结束后删除该库。
func TestCartFlow(t *testing.T) {
	path := os.Getenv("CART_TEST_CONFIG")
	if path == "" {
		t.Skip("设置 CART_TEST_CONFIG 为本机配置的绝对路径后运行")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := infra.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	adminSQL, _ := admin.DB()
	name := fmt.Sprintf("gin_mall_cart_check_%x", time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4").Error; err != nil {
		adminSQL.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE `" + name + "`").Error; err != nil {
			t.Errorf("删除本次检查库 %s: %v", name, err)
		}
		adminSQL.Close()
	})
	cfg.MySQL.Database, cfg.Admin.UserID, cfg.Session.CookieSecure = name, 0, false
	db, err := infra.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	for _, ensure := range []func() error{
		func() error { return account.EnsureSchema(ctx, db) },
		func() error { return catalog.EnsureSchema(ctx, db) },
		func() error { return cart.EnsureSchema(ctx, db) },
	} {
		if err := ensure(); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO categories (id, name) VALUES (1, '购物车检查分类')").Error; err != nil {
		t.Fatal(err)
	}
	// 运行真实的 server 进程，后面退出并重新启动，检查 MySQL 和登录会话保留。
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "server")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/server")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("编译服务: %v\n%s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Port = listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", cfg.Server.Port)
	start := func() func() {
		log, err := os.CreateTemp(tmp, "server-*.log")
		if err != nil {
			t.Fatal(err)
		}
		process := exec.Command(binary, "-config", configPath)
		process.Stdout, process.Stderr = log, log
		if err := process.Start(); err != nil {
			log.Close()
			t.Fatal(err)
		}
		stopped := false
		stop := func() {
			if !stopped {
				_ = process.Process.Kill()
				_ = process.Wait()
				log.Close()
				stopped = true
			}
		}
		t.Cleanup(stop)
		client := &http.Client{Timeout: time.Second}
		for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); {
			r, err := client.Get(base + "/api/products")
			if err == nil {
				r.Body.Close()
				if r.StatusCode == http.StatusOK {
					return stop
				}
			}
			time.Sleep(80 * time.Millisecond)
		}
		stop()
		output, _ := os.ReadFile(log.Name())
		t.Fatalf("服务未能启动: %s", output)
		return nil
	}
	stop := start()
	request := func(client *http.Client, method, path string, body any, want int) json.RawMessage {
		t.Helper()
		var data []byte
		if body != nil {
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequest(method, base+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		reply, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer reply.Body.Close()
		raw, err := io.ReadAll(reply.Body)
		if err != nil {
			t.Fatal(err)
		}
		if reply.StatusCode != want {
			t.Fatalf("%s %s: 希望 %d，得到 %d: %s", method, path, want, reply.StatusCode, raw)
		}
		var envelope struct {
			Data  json.RawMessage                 `json:"data"`
			Error *struct{ Code, Message string } `json:"error"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		if (want < 400 && envelope.Error != nil) || (want >= 400 && envelope.Error == nil) {
			t.Fatalf("data/error 响应不一致: %s", raw)
		}
		return envelope.Data
	}
	check := func(ok bool, message string) {
		t.Helper()
		if !ok {
			t.Fatal(message)
		}
	}
	anonymous := &http.Client{Timeout: 5 * time.Second}
	register := func(username string) (*http.Client, int64) {
		data := request(anonymous, "POST", "/api/auth/register", map[string]any{
			"username": username, "email": username + "@example.test", "password": "abcdef"}, 201)
		profile := decodeCartCheck[account.Profile](t, data)
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
		request(client, "POST", "/api/auth/login", map[string]any{"identifier": username, "password": "abcdef"}, 200)
		me := decodeCartCheck[account.Profile](t, request(client, "GET", "/api/users/me", nil, 200))
		check(me.ID == profile.ID, "本人资料身份不正确")
		return client, profile.ID
	}
	a, aID := register("cartA")
	b, bID := register("cartB")
	seller, sellerID := register("cartSell")
	if err := db.Exec("UPDATE users SET role = 'seller' WHERE id = ?", sellerID).Error; err != nil {
		t.Fatal(err)
	}
	request(seller, "POST", "/api/shops", map[string]any{"name": "购物车检查店铺"}, 201)
	product := decodeCartCheck[catalog.Product](t, request(seller, "POST", "/api/shops/me/products", map[string]any{
		"name": "水杯", "price": "20.00", "stock": 4, "category_id": 1}, 201))
	productPath := fmt.Sprintf("/api/shops/me/products/%d", product.ID)
	request(seller, "PATCH", productPath+"/status", map[string]any{"status": "on_sale"}, 200)
	itemsPath := "/api/cart/items"
	itemPath := fmt.Sprintf("%s/%d", itemsPath, product.ID)
	add := func(client *http.Client, quantity any, want int) cart.Result {
		return decodeCartCheck[cart.Result](t, request(client, "POST", itemsPath,
			map[string]any{"product_id": product.ID, "quantity": quantity, "user_id": bID}, want))
	}
	set := func(client *http.Client, quantity any, want int) cart.Result {
		return decodeCartCheck[cart.Result](t, request(client, "PATCH", itemPath,
			map[string]any{"quantity": quantity, "user_id": aID}, want))
	}
	list := func(client *http.Client) []cart.ItemView {
		data := request(client, "GET", fmt.Sprintf("%s?user_id=%d", itemsPath, aID), nil, 200)
		return decodeCartCheck[struct{ Items []cart.ItemView }](t, data).Items
	}
	saved := func() cart.Item {
		var row cart.Item
		if err := db.Where("user_id = ? AND product_id = ?", aID, product.ID).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	request(anonymous, "GET", itemsPath, nil, 401)
	zero := add(a, 0, 200)
	check(!zero.Changed && zero.Quantity == 0 && len(list(a)) == 0, "加入 0 创建了购物项")
	for _, bad := range []any{-1, 1.5, "2", nil} {
		add(a, bad, 400)
		set(a, bad, 400)
	}
	request(a, "POST", itemsPath, map[string]any{"product_id": product.ID}, 400)
	request(a, "PATCH", itemPath, map[string]any{}, 400)
	check(add(a, 2, 200).Quantity == 2, "首次加入数量不正确")
	capped := add(a, 3, 200)
	check(capped.Quantity == 4 && capped.Adjusted && capped.Message != "", "重复加入没有累加并提示库存截断")
	before := saved()
	add(a, 0, 200)
	list(a)
	check(saved().UpdatedAt.Equal(before.UpdatedAt), "加入 0 或查询修改了时间")
	check(len(list(b)) == 0, "伪造查询用户编号读取了他人购物车")
	set(b, 1, 404)
	set(b, 0, 404)
	request(b, "DELETE", fmt.Sprintf("%s?user_id=%d", itemPath, aID), nil, 404)
	check(saved().Quantity == 4, "他人能修改或移除购物项")
	check(add(b, 1, 200).Quantity == 1 && set(b, 2, 200).Quantity == 2 && saved().Quantity == 4,
		"同一商品的两人购物项未隔离")
	request(b, "DELETE", fmt.Sprintf("%s?user_id=%d", itemPath, aID), nil, 200)
	check(saved().Quantity == 4, "移除自己的同款商品影响了他人")
	t.Logf("归属核对：用户 %d 的商品 %d 数量为 4；用户 %d 改请求编号不能操作它", aID, product.ID, bID)
	request(seller, "PATCH", productPath, map[string]any{"stock": 1, "name": "新水杯", "price": "25.00"}, 200)
	shown := list(a)[0]
	check(shown.Quantity == 4 && shown.Product.Stock == 1 && shown.Product.Name == "新水杯" && shown.Product.Price == "25.00" &&
		shown.QuantityExceedsStock && saved().UpdatedAt.Equal(before.UpdatedAt), "查询没有保留原数量、读取最新商品信息或保持原时间")
	add(a, 1, 409)
	check(saved().Quantity == 4, "库存下降后再次加入擅自减少原数量")
	capped = set(a, 5, 200)
	check(capped.Quantity == 1 && capped.Adjusted && capped.Message != "", "主动修改超过库存没有截断并提示")
	request(seller, "PATCH", productPath, map[string]any{"stock": 4}, 200)
	check(set(a, int64(9223372036854775807), 200).Quantity == 4, "大整数没有安全截断")
	check(set(a, 2, 200).Quantity == 2, "主动修改未生效")
	request(seller, "PATCH", productPath+"/status", map[string]any{"status": "off_sale"}, 200)
	shown = list(a)[0]
	check(shown.Quantity == 2 && !shown.CanChangeQuantity && shown.Message == "已下架", "下架购物项没有保留并标明状态")
	add(a, 1, 409)
	set(a, 1, 409)
	check(set(a, 0, 200).Removed && len(list(a)) == 0, "下架后改为 0 没有移除")
	request(seller, "PATCH", productPath+"/status", map[string]any{"status": "on_sale"}, 200)
	add(a, 2, 200)
	request(seller, "PATCH", productPath, map[string]any{"stock": 0}, 200)
	shown = list(a)[0]
	check(shown.Quantity == 2 && !shown.CanChangeQuantity && shown.Message == "售罄", "售罄购物项没有保留并标明状态")
	add(a, 1, 409)
	set(a, 1, 409)
	request(a, "DELETE", itemPath, nil, 200)
	request(seller, "PATCH", productPath, map[string]any{"stock": 4}, 200)
	add(a, 2, 200)
	before = saved()
	var stock int64
	db.Table("products").Select("stock").Where("id = ?", product.ID).Scan(&stock)
	check(stock == 4, "购物车操作扣减了商品库存")
	stop()
	stop = start()
	check(list(a)[0].Quantity == 2 && saved().CreatedAt.Equal(before.CreatedAt) && saved().UpdatedAt.Equal(before.UpdatedAt),
		"服务进程重启后购物项、时间或登录会话未保留")
	t.Logf("重启核对：数据库 %s，user_id=%d，product_id=%d，quantity=2，商品库存=4", name, aID, product.ID)
	request(a, "POST", itemsPath, map[string]any{"product_id": 2147483647, "quantity": 1}, 404)
	request(a, "GET", "/api/products", nil, 200)
	request(a, "GET", fmt.Sprintf("/api/products/%d", product.ID), nil, 200)
	request(seller, "GET", "/api/shops/me/products", nil, 200)
	request(a, "DELETE", itemPath, nil, 200)
	check(len(list(a)) == 0, "移除后购物车不为空")
	t.Log("加入、修改、移除、查询、数量边界、下架/售罄与现有账号商品接口检查通过")
}

func decodeCartCheck[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
