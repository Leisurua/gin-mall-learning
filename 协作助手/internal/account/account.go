package account

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	ErrDuplicateAccount   = errors.New("用户名或邮箱已被使用")
	ErrInvalidCredentials = errors.New("登录标识或密码错误")
	ErrNotAuthenticated   = errors.New("请先登录或重新登录")
	ErrSessionUnavailable = errors.New("登录状态服务暂不可用")
	usernamePattern       = regexp.MustCompile(`^[A-Za-z0-9_]{1,8}$`)
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

type user struct {
	ID           int64 `gorm:"primaryKey"`
	Username     string
	Email        string
	PasswordHash string `json:"-"`
}

func (user) TableName() string { return "users" }

// Profile 单独定义接口可返回的数据，密码哈希留在 user 中。
type Profile struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type Service struct {
	db       *gorm.DB
	sessions *sessionStore
}

//go:embed schema.sql
var schemaSQL string

func EnsureSchema(ctx context.Context, db *gorm.DB) error {
	// 只创建尚不存在的用户表，已有商品表保持原有结构。
	if err := db.WithContext(ctx).Exec(schemaSQL).Error; err != nil {
		return fmt.Errorf("创建用户表: %w", err)
	}
	return nil
}

func NewService(db *gorm.DB, client *redis.Client, idle, maximum time.Duration) *Service {
	return &Service{db: db, sessions: &sessionStore{
		client: client, idle: idle, maximum: maximum,
	}}
}

func (s *Service) Register(ctx context.Context, username, email, password string) (Profile, error) {
	if !usernamePattern.MatchString(username) {
		return Profile{}, &ValidationError{"用户名须为 1 到 8 个英文字母、数字或下划线"}
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return Profile{}, err
	}
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 6 || len(password) > 72 {
		return Profile{}, &ValidationError{"密码至少 6 个字符，且不能超过 72 个 UTF-8 字节"}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Profile{}, fmt.Errorf("生成密码哈希: %w", err)
	}
	u := user{Username: username, Email: email, PasswordHash: string(hash)}
	// 唯一约束也参与查重，同时到达的注册请求不能绕过它。
	if err := s.db.WithContext(ctx).Create(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return Profile{}, ErrDuplicateAccount
		}
		return Profile{}, fmt.Errorf("保存用户: %w", err)
	}
	return profileOf(u), nil
}

func (s *Service) Login(ctx context.Context, identifier, password string) (Profile, Session, error) {
	if identifier == "" || password == "" {
		return Profile{}, Session{}, &ValidationError{"登录标识和密码不能为空"}
	}
	// 登录也显式检查字节上限，避免将超长输入交给 bcrypt。
	if len(password) > 72 {
		return Profile{}, Session{}, ErrInvalidCredentials
	}
	field := "username"
	if strings.Contains(identifier, "@") {
		field = "email"
		var err error
		identifier, err = normalizeEmail(identifier)
		if err != nil {
			return Profile{}, Session{}, ErrInvalidCredentials
		}
	}
	var u user
	err := s.db.WithContext(ctx).Where(field+" = ?", identifier).Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Profile{}, Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Profile{}, Session{}, fmt.Errorf("查询登录账号: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return Profile{}, Session{}, ErrInvalidCredentials
	}
	session, err := s.sessions.create(ctx, u.ID)
	if err != nil {
		return Profile{}, Session{}, err
	}
	return profileOf(u), session, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	return s.sessions.authenticate(ctx, token)
}

func (s *Service) Profile(ctx context.Context, userID int64) (Profile, error) {
	var u user
	err := s.db.WithContext(ctx).Select("id", "username", "email").First(&u, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Profile{}, ErrNotAuthenticated
	}
	if err != nil {
		return Profile{}, fmt.Errorf("查询本人资料: %w", err)
	}
	return profileOf(u), nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || len(value) > 254 || !strings.Contains(value, "@") {
		return "", &ValidationError{"邮箱格式不合法，长度最多 254 字节"}
	}
	return value, nil
}

func profileOf(u user) Profile {
	return Profile{ID: u.ID, Username: u.Username, Email: u.Email}
}
