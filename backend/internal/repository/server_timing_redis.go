package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/redis/go-redis/v9"
)

type serverTimingRedisHook struct{}

func (serverTimingRedisHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (serverTimingRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) (err error) {
		defer func() {
			if !errors.Is(err, redis.Nil) {
				selectionbudget.RecordError(ctx, err)
			}
		}()
		if err := selectionbudget.Take(ctx, "redis", 1); err != nil {
			cmd.SetErr(err)
			return err
		}
		if !servertiming.Active(ctx) {
			return next(ctx, cmd)
		}
		startedAt := time.Now()
		err = next(ctx, cmd)
		servertiming.Record(ctx, servertiming.MetricRedis, startedAt, time.Now(), 1)
		return err
	}
}

func (serverTimingRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) (err error) {
		defer func() {
			if !errors.Is(err, redis.Nil) {
				selectionbudget.RecordError(ctx, err)
			}
		}()
		if err := selectionbudget.Take(ctx, "redis", len(cmds)); err != nil {
			for _, cmd := range cmds {
				cmd.SetErr(err)
			}
			return err
		}
		if !servertiming.Active(ctx) {
			return next(ctx, cmds)
		}
		startedAt := time.Now()
		err = next(ctx, cmds)
		servertiming.Record(ctx, servertiming.MetricRedis, startedAt, time.Now(), len(cmds))
		return err
	}
}
