package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
)

// GetRuntimeRPM preserves the existing Redis natural-minute keys and counting
// algorithm, but does not turn malformed values/failed reads into zero.
func (c *RPMCacheImpl) GetRuntimeRPM(ctx context.Context, ids []int64) (map[int64]int, error) {
	suffix, err := c.currentMinuteSuffix(ctx)
	if err != nil {
		return nil, err
	}
	pipe := c.rdb.Pipeline()
	cmds := map[int64]*redis.StringCmd{}
	for _, id := range ids {
		cmds[id] = pipe.Get(ctx, fmt.Sprintf("%s%d:%s", rpmKeyPrefix, id, suffix))
	}
	if _, err = pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	out := map[int64]int{}
	for id, cmd := range cmds {
		n, e := cmd.Int()
		if errors.Is(e, redis.Nil) {
			n = 0
		} else if e != nil || n < 0 {
			return nil, fmt.Errorf("invalid runtime RPM")
		}
		out[id] = n
	}
	return out, nil
}
