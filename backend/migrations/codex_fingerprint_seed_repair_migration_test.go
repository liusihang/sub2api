package migrations

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexFingerprintSeedRepairMigration(t *testing.T) {
	raw, err := FS.ReadFile("251_fix_openai_apikey_codex_fingerprint_seed.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(raw)), " ")
	require.Contains(t, sql, "WHERE deleted_at IS NULL")
	require.Contains(t, sql, "AND platform = 'openai'")
	require.Contains(t, sql, "AND type IN ('oauth', 'setup-token', 'apikey')")
	require.Contains(t, sql, "COALESCE(extra->>'codex_fingerprint_mode', '') IN ('device', 'session', 'full')")
	require.Contains(t, sql, "extra->>'codex_fingerprint_seed' IS NULL OR NOT (")
	require.Contains(t, sql, "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
	require.Contains(t, sql, "extra->>'codex_fingerprint_seed' <> '00000000-0000-0000-0000-000000000000'")
	require.Contains(t, sql, "to_jsonb(gen_random_uuid()::text)")
	require.NotContains(t, sql, "'{codex_fingerprint_mode}'")
}

func TestHistoricalAPIKeyCodexFingerprintSeedMigrationUnchanged(t *testing.T) {
	raw, err := FS.ReadFile("234_backfill_codex_fingerprint_seed_for_api_keys.sql")
	require.NoError(t, err)
	require.Equal(t, "357f5b6943502c6d514bba2ddf7843c3ac630125fd61b0462590ac867641acd9", fmt.Sprintf("%x", sha256.Sum256(raw)))
}
