package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountRPMCacheStub struct {
	batchIDs   []int64
	batchRPM   map[int64]int
	currentRPM int
}

func (s *accountRPMCacheStub) IncrementRPM(context.Context, int64) (int, error) {
	return s.currentRPM, nil
}

func (s *accountRPMCacheStub) GetRPM(context.Context, int64) (int, error) {
	return s.currentRPM, nil
}

func (s *accountRPMCacheStub) GetRPMBatch(_ context.Context, accountIDs []int64) (map[int64]int, error) {
	s.batchIDs = append([]int64(nil), accountIDs...)
	return s.batchRPM, nil
}

func TestWithRPMPrefetchIncludesAPIKeyAccounts(t *testing.T) {
	cache := &accountRPMCacheStub{batchRPM: map[int64]int{2: 14, 3: 7}}
	svc := &GatewayService{rpmCache: cache}
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"base_rpm": 0}},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"base_rpm": 15}},
		{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"base_rpm": 10}},
		{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"base_rpm": 15}},
	}

	ctx := svc.withRPMPrefetch(context.Background(), accounts)
	count, ok := rpmFromPrefetchContext(ctx, 2)
	require.True(t, ok)
	require.Equal(t, []int64{2, 3}, cache.batchIDs)
	require.Equal(t, 14, count)
	count, ok = rpmFromPrefetchContext(ctx, 3)
	require.True(t, ok)
	require.Equal(t, 7, count)
}

func TestIsAccountSchedulableForRPMAppliesToAPIKey(t *testing.T) {
	cache := &accountRPMCacheStub{currentRPM: 15}
	svc := &GatewayService{rpmCache: cache}
	account := &Account{
		ID:       2,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra:    map[string]any{"base_rpm": 15},
	}

	require.False(t, svc.isAccountSchedulableForRPM(context.Background(), account, false))
	require.True(t, svc.isAccountSchedulableForRPM(context.Background(), account, true))

	cache.currentRPM = 18
	require.False(t, svc.isAccountSchedulableForRPM(context.Background(), account, false))
	require.False(t, svc.isAccountSchedulableForRPM(context.Background(), account, true))
}

func TestIsAccountSchedulableForRPMLeavesUnsupportedAccountTypesUnchanged(t *testing.T) {
	cache := &accountRPMCacheStub{currentRPM: 999}
	svc := &GatewayService{rpmCache: cache}
	account := &Account{
		ID:       5,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"base_rpm": 15},
	}

	require.True(t, svc.isAccountSchedulableForRPM(context.Background(), account, false))
	require.Empty(t, cache.batchIDs)
}
