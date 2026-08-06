package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompositeRouteBillingMultiplierOverridesCompositeDefault(t *testing.T) {
	rate := 0.4
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched:               true,
		TargetPlatform:        PlatformOpenAI,
		BillingRateMultiplier: &rate,
	})
	apiKey := &APIKey{Group: &Group{Platform: PlatformComposite}}

	got := effectiveCompositeBillingMultiplier(ctx, apiKey, 0.13)
	require.InDelta(t, 0.4, got, 1e-12)
}

func TestCompositeRouteBillingMultiplierFallsBackToCompositeDefault(t *testing.T) {
	ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched:        true,
		TargetPlatform: PlatformOpenAI,
	})
	apiKey := &APIKey{Group: &Group{Platform: PlatformComposite}}

	got := effectiveCompositeBillingMultiplier(ctx, apiKey, 0.13)
	require.InDelta(t, 0.13, got, 1e-12)
}
