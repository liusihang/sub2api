//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestCompositeRouteInflightReservationMatchesBillingRate(t *testing.T) {
	low, high, free := 0.0001, 0.4, 0.0
	for _, tc := range []struct {
		name     string
		platform string
		route    *float64
		want     float64
	}{
		{"low route", PlatformComposite, &low, low},
		{"high route", PlatformComposite, &high, high},
		{"free route", PlatformComposite, &free, 0},
		{"unset route", PlatformComposite, nil, 0.13},
		{"non composite ignores route", PlatformOpenAI, &low, 0.13},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID, price := int64(201), 0.5
			group := &Group{ID: groupID, Platform: tc.platform, RateMultiplier: 0.13,
				ModelPricing: []ChannelModelPricing{{Models: []string{"test-reservation"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price}}}
			key := &APIKey{GroupID: &groupID, Group: group, User: &User{ID: 1}}
			ctx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, TargetPlatform: PlatformOpenAI, BillingRateMultiplier: tc.route})
			svc := newInflightEstimateGateway(t, nil)
			estimate, priced := svc.EstimateInflightReservation(ctx, key, InflightEstimateRequest{Model: "test-reservation"})
			require.True(t, priced)
			require.InDelta(t, price*tc.want, estimate, 1e-12)
			textRate, _, _ := svc.recordUsageMultipliers(ctx, key, key.User, OpenAIPricingAtFromContext(ctx))
			require.InDelta(t, tc.want, textRate, 1e-12)
		})
	}
}

func TestCompositeRouteProfitGateUsesSettlementRateAcrossTurns(t *testing.T) {
	for _, rate := range []float64{0, 0.0001, 0.4} {
		billingGroup := &Group{ID: 201, Platform: PlatformComposite, Status: StatusActive, Hydrated: true, RateMultiplier: 0.13, SubscriptionType: SubscriptionTypeStandard}
		member := profitControlTestGroup(202, 0.25, 0)
		snapshot := NewSchedulerSnapshotService(nil, nil, nil, profitControlGroupRepo{group: member}, nil)
		base := context.WithValue(context.Background(), ctxkey.Group, billingGroup)
		base = WithCompositeRouteDecision(base, CompositeRouteDecision{Matched: true, TargetPlatform: PlatformOpenAI, BillingRateMultiplier: &rate})
		openaiSvc := &OpenAIGatewayService{schedulerSnapshot: snapshot}
		connCtx, _ := openaiSvc.WithOpenAIRequestPricingContext(base, &member.ID)
		gate, _ := connCtx.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate)
		require.NotNil(t, gate)
		require.InDelta(t, rate*0.75, gate.threshold, 1e-12)
		turnCtx, _ := openaiSvc.WithOpenAITurnPricingContext(connCtx, &billingGroup.ID)
		turnGate, _ := turnCtx.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate)
		require.NotNil(t, turnGate)
		require.InDelta(t, rate*0.75, turnGate.threshold, 1e-12)
		account := profitControlTestAccountWithRate(&Account{}, rate)
		vetoed, _ := OpenAIProfitControlVeto(turnCtx, account)
		require.Equal(t, rate > 0, vetoed, "the margin gate must use the route sale rate, including free routes")

		gatewayCtx, _ := WithGatewayTokenRequestPricing(base)
		gatewaySvc := &GatewayService{schedulerSnapshot: snapshot}
		gatewayCtx = gatewaySvc.withGatewayProfitControlGate(gatewayCtx, &member.ID)
		gatewayGate, _ := gatewayCtx.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate)
		require.NotNil(t, gatewayGate)
		require.InDelta(t, rate*0.75, gatewayGate.threshold, 1e-12)
	}
}
