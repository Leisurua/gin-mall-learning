package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const sessionKeyPrefix = "gin-mall:session:"

type Session struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
}

type sessionRecord struct {
	UserID            int64 `json:"user_id"`
	AbsoluteExpiresAt int64 `json:"absolute_expires_at"`
}

type sessionStore struct {
	client  *redis.Client
	idle    time.Duration
	maximum time.Duration
}

// 把读取、检查最长时限、续期作为一个 Redis 操作执行。
// 最长到期时间保留在原记录中，续期只改变 TTL。
var renewSession = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if not value then return nil end
local record = cjson.decode(value)
local remaining = tonumber(record.absolute_expires_at) - tonumber(ARGV[1])
if remaining <= 0 then
    redis.call('DEL', KEYS[1])
    return nil
end
local ttl = math.min(tonumber(ARGV[2]), remaining)
redis.call('EXPIRE', KEYS[1], ttl)
return {value, ttl}
`)

func (s *sessionStore) create(ctx context.Context, userID int64) (Session, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return Session{}, fmt.Errorf("生成 Session 凭据: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	now := time.Now().UTC().Truncate(time.Second)
	record := sessionRecord{UserID: userID, AbsoluteExpiresAt: now.Add(s.maximum).Unix()}
	value, err := json.Marshal(record)
	if err != nil {
		return Session{}, fmt.Errorf("编码 Session: %w", err)
	}
	ttl := min(s.idle, s.maximum)
	if err := s.client.Set(ctx, sessionKey(token), value, ttl).Err(); err != nil {
		return Session{}, fmt.Errorf("%w: %w", ErrSessionUnavailable, err)
	}
	return Session{Token: token, UserID: userID, ExpiresAt: now.Add(ttl)}, nil
}

func (s *sessionStore) authenticate(ctx context.Context, token string) (Session, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || len(token) != 43 {
		return Session{}, ErrNotAuthenticated
	}
	now := time.Now().UTC().Truncate(time.Second)
	result, err := renewSession.Run(ctx, s.client, []string{sessionKey(token)},
		now.Unix(), int64(s.idle/time.Second)).Result()
	if errors.Is(err, redis.Nil) {
		return Session{}, ErrNotAuthenticated
	}
	if err != nil {
		return Session{}, fmt.Errorf("%w: %w", ErrSessionUnavailable, err)
	}
	parts, ok := result.([]interface{})
	if !ok || len(parts) != 2 {
		return Session{}, fmt.Errorf("%w: Session 返回格式错误", ErrSessionUnavailable)
	}
	value, valueOK := parts[0].(string)
	ttl, ttlOK := parts[1].(int64)
	var record sessionRecord
	if !valueOK || !ttlOK || ttl <= 0 || json.Unmarshal([]byte(value), &record) != nil || record.UserID <= 0 {
		return Session{}, ErrNotAuthenticated
	}
	return Session{Token: token, UserID: record.UserID, ExpiresAt: now.Add(time.Duration(ttl) * time.Second)}, nil
}

func sessionKey(token string) string {
	// Redis 键使用凭据的摘要，原始凭据只交给客户端。
	digest := sha256.Sum256([]byte(token))
	return sessionKeyPrefix + hex.EncodeToString(digest[:])
}
