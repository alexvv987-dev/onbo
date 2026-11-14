package backend

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"onbo/internal/config"
)

const (
	chatLimitKeyPrefix     = "onbo:limit:"
	consumeChatQuotaScript = "local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end; return n"
	sessionKeyPrefix       = "onbo:session:"
	sessionTokenLength     = identifierByteCount * 2
)

type Sessions interface {
	CreateSession(context.Context, string) (string, error)
	GetSessionUserID(context.Context, string) (string, error)
	DeleteSession(context.Context, string) error
	ConsumeChatQuota(context.Context, string) (bool, error)
	Ping(context.Context) error
}
type RedisSessions struct {
	Client   *redis.Client
	settings config.Redis
}

func NewRedisSessions(address, password string, options ...config.Redis) *RedisSessions {
	settings := config.DefaultRedis()
	if len(options) > 0 {
		settings = options[0]
	}
	return &RedisSessions{Client: redis.NewClient(&redis.Options{Addr: address, Password: password, DialTimeout: settings.DialTimeout, ReadTimeout: settings.ReadTimeout, WriteTimeout: settings.WriteTimeout}), settings: settings}
}
func (sessions *RedisSessions) CreateSession(ctx context.Context, userID string) (string, error) {
	token := generateIdentifier()
	err := sessions.Client.Set(ctx, sessionKeyPrefix+token, userID, sessions.settings.SessionTTL).Err()
	return token, err
}
func (sessions *RedisSessions) GetSessionUserID(ctx context.Context, token string) (string, error) {
	if len(token) != sessionTokenLength {
		return "", ErrForbidden
	}
	user, err := sessions.Client.Get(ctx, sessionKeyPrefix+token).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrForbidden
	}
	return user, err
}
func (sessions *RedisSessions) DeleteSession(ctx context.Context, token string) error {
	return sessions.Client.Del(ctx, sessionKeyPrefix+token).Err()
}
func (sessions *RedisSessions) ConsumeChatQuota(ctx context.Context, userID string) (bool, error) {
	windowMilliseconds := sessions.settings.RateWindow.Milliseconds()
	if sessions.settings.RateWindow%time.Millisecond != 0 {
		windowMilliseconds++
	}
	messageCount, err := sessions.Client.Eval(ctx, consumeChatQuotaScript, []string{chatLimitKeyPrefix + userID}, windowMilliseconds).Int64()
	return messageCount <= int64(sessions.settings.ChatRateLimit), err
}
func (sessions *RedisSessions) Ping(ctx context.Context) error {
	return sessions.Client.Ping(ctx).Err()
}
func (sessions *RedisSessions) Close() error { return sessions.Client.Close() }
