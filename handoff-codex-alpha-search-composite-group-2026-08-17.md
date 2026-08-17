# Codex alpha search for composite groups handoff

## Goal

Allow Codex `/alpha/search` requests made with a `composite` group to use an OpenAI target route, while preserving rejection for concrete non-OpenAI groups and composite routes resolved to another platform.

## Authoritative surfaces

- Source worktree: `/Users/liusihang/Documents/ChatGPT/Sub2ApiWorktrees/liusihang-main-sse-composite`
- Branch: `codex/update-upstream-20260815`
- Starting commit: `ddc7bd77a43576715dd4145f69852582bfba4965`
- Live image recorded by the user: `sub2api:0.1.177-fingerprint-ddc7bd77-live-20260816`
- This task does not modify group data, API keys, live containers, or deployment configuration.

## Completed actions

- Confirmed the source worktree is clean and its starting commit matches the live image source tag.
- Traced all three Codex alpha-search routes through `compositeTargetPlatformMiddleware`.
- Confirmed the middleware resolves a composite request model to a concrete platform and stores it in request context before `OpenAIGatewayHandler.AlphaSearch` runs.
- Identified that `AlphaSearch` still rejects solely on the raw group platform.
- Added a route-level regression test covering all three alpha-search aliases for a composite request resolved to OpenAI.
- Ran the focused regression test and confirmed the current code returned `404` on `/v1/alpha/search` instead of entering the handler's later stages.
- Updated the handler gate to accept only concrete OpenAI groups or composite requests already resolved to OpenAI.
- Added a negative route test preserving rejection for a composite request resolved to Anthropic.
- Read back the live database without exposing API-key or account credentials. The currently active `Self` API key belongs to group `13`, `Codex Personal OpenAI Compatible (Private)`, whose platform is `composite`.
- Confirmed live group `13` has explicit `endpoint=any` OpenAI routes for `gpt-5.6-luna`, `gpt-5.6-sol`, and `gpt-5.6-terra`; no endpoint-classification change is needed for the current bug.
- Confirmed group `13` currently has six schedulable OpenAI API-key accounts whose configured/default capability policy is eligible for `alpha_search` under the existing account capability contract.

## Checkpoint

The minimal source fix and regression coverage are complete and committed on `codex/update-upstream-20260815`. Full unit, build, and targeted vet verification passed.

## Verification results

- Worktree status before edits: clean.
- Registered source HEAD: `ddc7bd77a43576715dd4145f69852582bfba4965`.
- Red command:
  `go test -tags=unit ./internal/server/routes -run TestGatewayRoutesAlphaSearchAllowsCompositeOpenAIRoute -count=1`
- Red result: failed as expected with status mismatch `expected: 500`, `actual: 404`, first observed on `/v1/alpha/search`.
- Focused green command:
  `go test -tags=unit ./internal/server/routes -run 'TestGatewayRoutesAlphaSearch' -count=1`
- Focused green result: passed.
- Related focused suites passed for Composite route resolution, handler platform resolution, and OpenAI alpha-search account capability scheduling.
- Full backend unit suite passed:
  `go test -tags=unit ./... -count=1`
- Full backend build passed:
  `go build ./...`
- Targeted static analysis passed:
  `go vet ./internal/handler ./internal/server/routes ./internal/service`
- No temporary `[DEBUG-*]` instrumentation is present in the changed code.

## Current status

Source task complete. No live service, database row, API key, account, or routing configuration has been modified.

## Next step

Deployment and a real Codex `web_search=live` acceptance request remain a separately authorized step. Build from the committed source, preserve the current live image as the rollback anchor, then verify health plus an authenticated `/alpha/search` request through group `13`.
