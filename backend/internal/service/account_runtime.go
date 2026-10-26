package service

import (
	"context"
	"fmt"
	"time"
)

// AccountRuntimeTokens counts recorded provider usage, not estimated in-flight
// tokens or customer billing multipliers. Strings preserve aggregate precision.
type AccountRuntimeTokens struct {
	Tokens         string `json:"tokens"`
	UncachedTokens string `json:"uncached_tokens"`
}
type accountRuntimeTokenReader interface {
	GetAccountRuntimeTokens(context.Context, []int64, time.Time, time.Time) (map[int64]AccountRuntimeTokens, error)
}

type RuntimeRPMReader interface {
	GetRuntimeRPM(context.Context, []int64) (map[int64]int, error)
}

func (s *ConcurrencyService) GetRuntimeConcurrency(ctx context.Context, ids []int64) (map[int64]int, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("runtime concurrency unavailable")
	}
	return s.cache.GetAccountConcurrencyBatch(ctx, ids)
}

func (s *AccountUsageService) GetAccountRuntimeTokens(ctx context.Context, ids []int64, start, end time.Time) (map[int64]AccountRuntimeTokens, error) {
	if s == nil {
		return nil, fmt.Errorf("runtime token source unavailable")
	}
	reader, ok := s.usageLogRepo.(accountRuntimeTokenReader)
	if !ok {
		return nil, fmt.Errorf("runtime token source unavailable")
	}
	return reader.GetAccountRuntimeTokens(ctx, ids, start, end)
}
