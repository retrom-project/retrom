package temporary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"retrom/internal/model"
)

type Redis struct {
	client *redis.Client
	prefix string
}

func New(address, prefix string) (*Redis, error) {
	if !strings.Contains(address, "://") {
		address = "redis://" + address
	}
	options, err := redis.ParseURL(address)
	if err != nil {
		return nil, fmt.Errorf("Redis configuration: %w", err)
	}
	options.PoolSize = 20
	options.DialTimeout = 3 * time.Second
	options.ReadTimeout = 5 * time.Second
	options.WriteTimeout = 5 * time.Second
	options.ContextTimeoutEnabled = true
	options.MaxRetries = 0
	return &Redis{client: redis.NewClient(options), prefix: prefix}, nil
}
func (r *Redis) Close() error { return temporaryError(r.client.Close()) }
func (r *Redis) Get(ctx context.Context, key string) (string, error) {
	value, err := r.client.Get(ctx, r.prefix+key).Result()
	if errors.Is(err, redis.Nil) {
		return "", model.ErrContextExpired
	}
	return value, temporaryError(err)
}

func (r *Redis) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if len(value) > 8*1024*1024 || ttl <= 0 {
		return model.ErrInvalid
	}
	return temporaryError(r.client.Set(ctx, r.prefix+key, value, ttl).Err())
}

func (r *Redis) Delete(ctx context.Context, key string) error {
	return temporaryError(r.client.Del(ctx, r.prefix+key).Err())
}

func (r *Redis) PutHash(ctx context.Context, key string, values map[string]string, ttl time.Duration) error {
	if ttl <= 0 || len(values) > 20000 {
		return model.ErrInvalid
	}
	total := 0
	for field, value := range values {
		total += len(field) + len(value)
	}
	if total > 8*1024*1024 {
		return model.ErrInvalid
	}
	_, err := r.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, r.prefix+key, values)
		pipe.Expire(ctx, r.prefix+key, ttl)
		return nil
	})
	return temporaryError(err)
}

func (r *Redis) HashValue(ctx context.Context, key, field string, ttl time.Duration) (string, error) {
	const script = `if redis.call('EXISTS',KEYS[1])==0 then return {0,''} end;
 local v=redis.call('HGET',KEYS[1],ARGV[1]);
 if not v then return {1,''} end;
 redis.call('PEXPIRE',KEYS[1],ARGV[2]); return {1,v}`
	result, err := r.client.Eval(ctx, script, []string{r.prefix + key}, field, ttl.Milliseconds()).Slice()
	if err != nil {
		return "", temporaryError(err)
	}
	if len(result) != 2 {
		return "", model.ErrUnavailable
	}
	if exists, ok := result[0].(int64); !ok || exists == 0 {
		return "", model.ErrContextExpired
	}
	value, ok := result[1].(string)
	if !ok {
		return "", model.ErrUnavailable
	}
	if value == "" {
		return "", model.ErrNotFound
	}
	return value, nil
}
func (r *Redis) Ping(ctx context.Context) error { return temporaryError(r.client.Ping(ctx).Err()) }
func (r *Redis) Limit(ctx context.Context, subject string, maximum int) error {
	const script = `local n=redis.call('INCR',KEYS[1]);
 if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end; return n`
	count, err := r.client.Eval(ctx, script, []string{r.prefix + "limit:" + subject}, 900000).Int64()
	if err != nil {
		return temporaryError(err)
	}
	if count > int64(maximum) {
		return model.ErrRateLimited
	}
	return nil
}

func temporaryError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("temporary store: %w: %w", model.ErrUnavailable, err)
}
