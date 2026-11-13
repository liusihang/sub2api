//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"maps"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func requireCanonicalUUIDString(t *testing.T, value string) {
	t.Helper()
	parsed, err := uuid.Parse(value)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, parsed)
	require.Equal(t, parsed.String(), value)
}

func TestMigration225BackfillsOnlyEnabledOpenAIOAuthMissingOrMalformedSeeds(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("225_backfill_codex_fingerprint_seed.sql")
	require.NoError(t, err)

	var missingID, blankID, malformedID, validID, offID, apiKeyID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-missing', 'openai', 'oauth', '{"codex_fingerprint_mode":"session"}'::jsonb)
RETURNING id
`).Scan(&missingID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-blank', 'openai', 'oauth', '{"codex_fingerprint_mode":"device","codex_fingerprint_seed":""}'::jsonb)
RETURNING id
`).Scan(&blankID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-malformed', 'openai', 'oauth', '{"codex_fingerprint_mode":"full","codex_fingerprint_seed":"BAD"}'::jsonb)
RETURNING id
`).Scan(&malformedID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-valid', 'openai', 'oauth', '{"codex_fingerprint_mode":"session","codex_fingerprint_seed":"11111111-1111-4111-8111-111111111111"}'::jsonb)
RETURNING id
`).Scan(&validID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-off', 'openai', 'oauth', '{"codex_fingerprint_mode":"off"}'::jsonb)
RETURNING id
`).Scan(&offID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-apikey', 'openai', 'apikey', '{"codex_fingerprint_mode":"session"}'::jsonb)
RETURNING id
`).Scan(&apiKeyID))

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	seedsAfterFirst := map[int64]string{}
	for _, id := range []int64{missingID, blankID, malformedID, validID} {
		var seed string
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra->>'codex_fingerprint_seed' FROM accounts WHERE id = $1`, id).Scan(&seed))
		requireCanonicalUUIDString(t, seed)
		seedsAfterFirst[id] = seed
	}
	require.Equal(t, "11111111-1111-4111-8111-111111111111", seedsAfterFirst[validID])

	for _, id := range []int64{offID, apiKeyID} {
		var hasSeed bool
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra ? 'codex_fingerprint_seed' FROM accounts WHERE id = $1`, id).Scan(&hasSeed))
		require.False(t, hasSeed)
	}

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	for id, want := range seedsAfterFirst {
		var got string
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra->>'codex_fingerprint_seed' FROM accounts WHERE id = $1`, id).Scan(&got))
		require.Equal(t, want, got)
	}
}

func TestBulkUpdateGeneratesDistinctStableCodexFingerprintSeedsPerEligibleRow(t *testing.T) {
	ctx := context.Background()
	testName := "bulk-codex-seed-" + uuid.NewString()
	type fixture struct {
		name        string
		accountType string
		extra       string
	}
	fixtures := []fixture{
		{name: testName + "-missing-a", accountType: service.AccountTypeAPIKey, extra: `{}`},
		{name: testName + "-missing-b", accountType: service.AccountTypeAPIKey, extra: `{"codex_fingerprint_seed":"BAD"}`},
		{name: testName + "-valid", accountType: service.AccountTypeOAuth, extra: `{"codex_fingerprint_seed":"11111111-1111-4111-8111-111111111111"}`},
		{name: testName + "-setup-token", accountType: service.AccountTypeSetupToken, extra: `{}`},
	}

	ids := make([]int64, 0, len(fixtures))
	for _, f := range fixtures {
		var id int64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ($1, 'openai', $2, $3::jsonb)
RETURNING id
`, f.name, f.accountType, f.extra).Scan(&id))
		ids = append(ids, id)
	}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM scheduler_outbox WHERE account_id = ANY($1)`, pq.Array(ids))
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = ANY($1)`, pq.Array(ids))
	})

	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	updates := service.AccountBulkUpdate{
		Extra: map[string]any{
			"codex_fingerprint_mode": "session",
		},
		EnsureCodexFingerprintSeed: true,
	}
	rows, err := repo.BulkUpdate(ctx, ids, updates)
	require.NoError(t, err)
	require.Equal(t, int64(len(ids)), rows)

	readSeed := func(id int64) string {
		t.Helper()
		var seed string
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COALESCE(extra->>'codex_fingerprint_seed', '') FROM accounts WHERE id = $1`, id).Scan(&seed))
		return seed
	}
	firstSeeds := []string{readSeed(ids[0]), readSeed(ids[1]), readSeed(ids[2]), readSeed(ids[3])}
	requireCanonicalUUIDString(t, firstSeeds[0])
	requireCanonicalUUIDString(t, firstSeeds[1])
	require.NotEqual(t, firstSeeds[0], firstSeeds[1], "gen_random_uuid must be evaluated per eligible row")
	require.Equal(t, "11111111-1111-4111-8111-111111111111", firstSeeds[2])
	requireCanonicalUUIDString(t, firstSeeds[3])
	require.NotEqual(t, firstSeeds[0], firstSeeds[3])

	rows, err = repo.BulkUpdate(ctx, ids, updates)
	require.NoError(t, err)
	require.Equal(t, int64(len(ids)), rows)
	for i, want := range firstSeeds {
		require.Equal(t, want, readSeed(ids[i]), "retry must not rotate an existing valid seed")
	}
}

type codexSeedRegressionFixture struct {
	name        string
	platform    string
	accountType string
	extra       map[string]any
	deleted     bool
	repair      bool
}

func codexSeedRegressionFixtures() []codexSeedRegressionFixture {
	var fixtures []codexSeedRegressionFixture
	for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeAPIKey} {
		for _, state := range []struct {
			name   string
			seed   any
			repair bool
		}{
			{name: "missing", repair: true},
			{name: "null", repair: true},
			{name: "blank", seed: "", repair: true},
			{name: "whitespace", seed: "   ", repair: true},
			{name: "malformed", seed: "BAD", repair: true},
			{name: "nil-uuid", seed: "00000000-0000-0000-0000-000000000000", repair: true},
			{name: "valid", seed: "11111111-1111-4111-8111-111111111111"},
		} {
			for _, mode := range []string{"device", "session", "full"} {
				extra := map[string]any{"codex_fingerprint_mode": mode, "custom": "preserved"}
				if state.name != "missing" {
					extra["codex_fingerprint_seed"] = state.seed
				}
				fixtures = append(fixtures, codexSeedRegressionFixture{
					name: accountType + "/" + state.name + "/" + mode, platform: service.PlatformOpenAI,
					accountType: accountType, extra: extra, repair: state.repair,
				})
			}
		}
		for _, mode := range []string{"off", "", "invalid", "unset"} {
			for _, seed := range []string{"", "11111111-1111-4111-8111-111111111111"} {
				extra := map[string]any{"custom": "preserved"}
				if mode != "unset" {
					extra["codex_fingerprint_mode"] = mode
				}
				if seed != "" {
					extra["codex_fingerprint_seed"] = seed
				}
				fixtures = append(fixtures, codexSeedRegressionFixture{
					name: accountType + "/disabled/" + mode + "/" + seed, platform: service.PlatformOpenAI,
					accountType: accountType, extra: extra,
				})
			}
		}
		fixtures = append(fixtures,
			codexSeedRegressionFixture{name: accountType + "/deleted", platform: service.PlatformOpenAI, accountType: accountType, deleted: true,
				extra: map[string]any{"codex_fingerprint_mode": "session", "custom": "preserved"}},
			codexSeedRegressionFixture{name: accountType + "/non-openai", platform: service.PlatformAnthropic, accountType: accountType,
				extra: map[string]any{"codex_fingerprint_mode": "session", "custom": "preserved"}},
		)
	}
	return fixtures
}

type codexSeedTestDB interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func createCodexSeedRegressionAccount(t *testing.T, db codexSeedTestDB, f *codexSeedRegressionFixture) int64 {
	t.Helper()
	raw, err := json.Marshal(f.extra)
	require.NoError(t, err)
	var id int64
	var persistedRaw []byte
	require.NoError(t, db.QueryRowContext(context.Background(), `
INSERT INTO accounts (name, platform, type, extra, deleted_at)
VALUES ($1, $2, $3, $4::jsonb, CASE WHEN $5 THEN NOW() ELSE NULL END)
RETURNING id, extra`, "codex-seed-"+uuid.NewString(), f.platform, f.accountType, string(raw), f.deleted).Scan(&id, &persistedRaw))
	if db == integrationDB {
		t.Cleanup(func() {
			_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM scheduler_outbox WHERE account_id = $1 OR payload->'account_ids' @> jsonb_build_array($1::bigint)`, id)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = $1`, id)
			require.NoError(t, err)
		})
	}
	var persistedExtra map[string]any
	require.NoError(t, json.Unmarshal(persistedRaw, &persistedExtra))
	for key, value := range f.extra {
		require.Contains(t, persistedExtra, key)
		require.Equal(t, value, persistedExtra[key], "insert must preserve fixture field %s", key)
	}
	// The long-context billing trigger adds defaults to OpenAI accounts.
	// Preserve the complete stored extra as the pre-operation baseline so the
	// assertions below also detect changes to those database-managed fields.
	f.extra = persistedExtra
	return id
}

func assertCodexSeedRegressionAccount(t *testing.T, db codexSeedTestDB, id int64, f codexSeedRegressionFixture) string {
	t.Helper()
	var raw []byte
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT extra FROM accounts WHERE id = $1`, id).Scan(&raw))
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	want := maps.Clone(f.extra)
	seed, _ := got["codex_fingerprint_seed"].(string)
	if f.repair {
		requireCanonicalUUIDString(t, seed)
		want["codex_fingerprint_seed"] = seed
	}
	require.Equal(t, want, got, "must preserve the mode and unrelated extra fields")
	return seed
}

func TestMigration251RepairsSupportedOpenAISeedsWithoutEnablingOrRotating(t *testing.T) {
	tx := testTx(t)
	raw, err := dbmigrations.FS.ReadFile("251_fix_openai_apikey_codex_fingerprint_seed.sql")
	require.NoError(t, err)
	fixtures := codexSeedRegressionFixtures()
	ids := make([]int64, len(fixtures))
	for i := range fixtures {
		ids[i] = createCodexSeedRegressionAccount(t, tx, &fixtures[i])
	}
	_, err = tx.ExecContext(context.Background(), string(raw))
	require.NoError(t, err)
	seeds := make([]string, len(fixtures))
	seen := make(map[string]bool)
	for i, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			seeds[i] = assertCodexSeedRegressionAccount(t, tx, ids[i], f)
			if f.repair {
				require.False(t, seen[seeds[i]], "each repaired account needs its own seed")
				seen[seeds[i]] = true
			}
		})
	}
	_, err = tx.ExecContext(context.Background(), string(raw))
	require.NoError(t, err)
	for i, f := range fixtures {
		require.Equal(t, seeds[i], assertCodexSeedRegressionAccount(t, tx, ids[i], f), "rerun must not rotate seeds: %s", f.name)
	}
}

func TestBulkUpdateCodexFingerprintSeedRegressionMatrix(t *testing.T) {
	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	for _, f := range codexSeedRegressionFixtures() {
		t.Run(f.name, func(t *testing.T) {
			id := createCodexSeedRegressionAccount(t, integrationDB, &f)
			updates := map[string]any{"custom": "preserved"}
			if mode, ok := f.extra["codex_fingerprint_mode"]; ok {
				updates["codex_fingerprint_mode"] = mode
			}
			var seed string
			for attempt := 0; attempt < 2; attempt++ {
				rows, err := repo.BulkUpdate(context.Background(), []int64{id}, service.AccountBulkUpdate{Extra: updates, EnsureCodexFingerprintSeed: true})
				require.NoError(t, err)
				if f.deleted {
					require.Zero(t, rows)
				} else {
					require.EqualValues(t, 1, rows)
				}
				got := assertCodexSeedRegressionAccount(t, integrationDB, id, f)
				if attempt > 0 {
					require.Equal(t, seed, got, "repeated updates must not rotate seeds")
				}
				seed = got
			}
		})
	}
}

func TestUpdateExtraCodexFingerprintSeedRegressionMatrix(t *testing.T) {
	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	for _, f := range codexSeedRegressionFixtures() {
		t.Run(f.name, func(t *testing.T) {
			id := createCodexSeedRegressionAccount(t, integrationDB, &f)
			updates := map[string]any{"custom": "preserved"}
			if mode, ok := f.extra["codex_fingerprint_mode"]; ok {
				updates["codex_fingerprint_mode"] = mode
			}
			var seed string
			for attempt := 0; attempt < 2; attempt++ {
				err := repo.UpdateExtra(context.Background(), id, updates)
				if f.deleted {
					require.ErrorIs(t, err, service.ErrAccountNotFound)
				} else {
					require.NoError(t, err)
				}
				got := assertCodexSeedRegressionAccount(t, integrationDB, id, f)
				if attempt > 0 {
					require.Equal(t, seed, got, "repeated updates must not rotate seeds")
				}
				seed = got
			}
		})
	}
}

func TestCodexFingerprintSeedDisableReenablePreservesSeed(t *testing.T) {
	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeAPIKey} {
		for _, operation := range []string{"bulk", "update-extra"} {
			t.Run(accountType+"/"+operation, func(t *testing.T) {
				f := codexSeedRegressionFixture{platform: service.PlatformOpenAI, accountType: accountType,
					extra: map[string]any{"codex_fingerprint_mode": "off", "custom": "preserved"}}
				id := createCodexSeedRegressionAccount(t, integrationDB, &f)
				var seed string
				for _, mode := range []string{"off", "session", "off", "full"} {
					updates := map[string]any{"codex_fingerprint_mode": mode}
					if operation == "bulk" {
						rows, err := repo.BulkUpdate(context.Background(), []int64{id}, service.AccountBulkUpdate{
							Extra: updates, EnsureCodexFingerprintSeed: service.ShouldEnsureCodexFingerprintSeedForExtraUpdates(updates),
						})
						require.NoError(t, err)
						require.EqualValues(t, 1, rows)
					} else {
						require.NoError(t, repo.UpdateExtra(context.Background(), id, updates))
					}
					f.extra["codex_fingerprint_mode"] = mode
					f.repair = mode != "off"
					got := assertCodexSeedRegressionAccount(t, integrationDB, id, f)
					if seed != "" {
						require.Equal(t, seed, got, "disable/re-enable must preserve the original seed")
					}
					if got != "" {
						seed = got
						f.extra["codex_fingerprint_seed"] = seed
					}
				}
			})
		}
	}
}
