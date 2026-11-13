-- Repair the historical API Key seed backfill's incorrect 'api_key' type.
-- Cover all supported OpenAI account types without enabling convergence or
-- rotating valid seeds. Keep historical migration contents and checksums intact.
UPDATE accounts
SET extra = jsonb_set(
    COALESCE(extra, '{}'::jsonb),
    '{codex_fingerprint_seed}',
    to_jsonb(gen_random_uuid()::text),
    true
)
WHERE deleted_at IS NULL
  AND platform = 'openai'
  AND type IN ('oauth', 'setup-token', 'apikey')
  AND COALESCE(extra->>'codex_fingerprint_mode', '') IN ('device', 'session', 'full')
  AND (
      extra->>'codex_fingerprint_seed' IS NULL
      OR NOT (
          extra->>'codex_fingerprint_seed' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
          AND extra->>'codex_fingerprint_seed' <> '00000000-0000-0000-0000-000000000000'
      )
  );
