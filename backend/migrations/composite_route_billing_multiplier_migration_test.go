package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompositeRouteBillingMultiplierMigrationIsIdempotent(t *testing.T) {
	raw, err := FS.ReadFile("194_composite_route_billing_rate_multiplier.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(raw))
	require.Contains(t, sql, "add column if not exists billing_rate_multiplier")
	require.Contains(t, sql, "from pg_constraint")
	require.Contains(t, sql, "composite_model_routes_billing_rate_multiplier_nonnegative")
}
