# Sub2API and CLIProxy Isolated Upgrade Handoff

## Goal

Upgrade Sub2API on top of the user-selected
`fork/codex/update-upstream-20260815` branch, preserve the existing custom
changes, and validate the upgraded Sub2API together with the CLIProxy
fingerprint hook/plugin without changing production services.

## Delivery Boundary

- Work only in:
  - `/Users/liusihang/Documents/ChatGPT/Sub2ApiWorktrees/liusihang-main-sse-composite`
  - `/Users/liusihang/Documents/ChatGPT/CLIProxyAPI-fingerprint-v7.2.133`
- Fetch and integrate current upstream Sub2API into the local tracking branch.
- Do not push the Fork branch unless separately authorized.
- Build and test final Linux amd64 candidate artifacts.
- On `aiserver`, use unique temporary containers, networks, ports, config,
  credentials, databases, caches, logs, and capture state.
- Do not edit, restart, recreate, reconnect, or route traffic away from the
  live Sub2API or CLIProxy containers.
- Do not write the live Sub2API database or Redis.
- Do not reuse the live CLIProxy OAuth auth directory in a concurrently
  running candidate.
- Remove all temporary remote resources after evidence is captured.

## Truth Surface

- Sub2API worktree:
  `/Users/liusihang/Documents/ChatGPT/Sub2ApiWorktrees/liusihang-main-sse-composite`
- Sub2API branch: `codex/update-upstream-20260815`
- Sub2API tracking branch: `fork/codex/update-upstream-20260815`
- Starting Sub2API SHA:
  `9f1ad42adacfad881e5f92fe00e113e959ccc9e2`
- CLIProxy worktree:
  `/Users/liusihang/Documents/ChatGPT/CLIProxyAPI-fingerprint-v7.2.133`
- CLIProxy branch: `codex/fingerprint-plugin-hook`
- CLIProxy HEAD:
  `d3df2a608c56c9069a2f42b85518ede3be74b8c2`
- CLIProxy implementation commit:
  `1e0fd536e6ddb45c776089415eb1e884a5390129`
- Remote test host: `aiserver`
- Production truth: exact live container/image IDs, restart counts, config
  hashes, and account readback captured before and after isolated testing.

## Acceptance

- The selected Sub2API branch contains the current upstream release plus all
  existing custom commits.
- Sub2API backend full tests, unit-tag tests, build, frontend frozen install,
  frontend build, and `git diff --check` pass.
- Final Linux amd64 Sub2API, CLIProxy, and plugin artifacts have immutable
  version/SHA records.
- Isolated end-to-end validation covers:
  - HTTP non-streaming Responses
  - HTTP/SSE Responses
  - native Responses WebSocket handshake and connection reuse
  - macOS and Windows platform profiles
  - deterministic IDs across candidate restart
  - concurrent request isolation
  - actual production-used model names against the controlled upstream
  - existing CLIProxy plugin coexistence
- Final upstream captures contain exactly two platform installation IDs while
  preserving distinct client session, thread, and prompt-cache identifiers.
- Temporary containers, images, networks, state, certificates, and credentials
  are removed.
- Live before/after normalized snapshots are identical.

## Current Checkpoint

- The requested local Sub2API branch started from the Fork tracking SHA
  `9f1ad42a`.
- A local rollback branch,
  `codex/backup-before-upstream-20260816-1806`, points to that starting SHA.
- Official upstream was fetched with an explicit `main` refspec and tags.
  The latest release remains `v0.1.177`; `origin/main` is
  `baeac1f3de21d37b129405f092ef86c24b3f203d`.
- The sole upstream post-tag commit is a VERSION sync whose stable patch ID is
  identical to the branch's existing `f9187355` commit.
- `origin/main` was merged locally without tree changes. Current Sub2API HEAD
  is `4f29a58d3a3dd97b5c2a9ab5478acf2337eebfa9`, and `origin/main` is now an
  ancestor.
- The worktree is clean except for this handoff.
- CLIProxy remains at the previously validated clean fingerprint branch.
- The `aiserver` production baseline was captured at
  `2026-08-16T18:06:11+08:00`:
  - Sub2API container
    `0870f05e742fd8740126327ef195dcd5d365ebf3f88118b896f3c03806c794d4`,
    image
    `sha256:2f85acaca3d714af95146b44f766de29ae570ba9d9282c37e7bb759b5bb6f911`,
    healthy, restart count `0`.
  - CLIProxy container
    `f53b49ea1fc3a5f25b32af01e4488b6bb64d3ebdd495185010768ce0a50762df`,
    image
    `sha256:74026e08cd32228989e0b4b123140bdf46d99b5785402844fd3c14e86574b0f7`,
    running, restart count `0`.
  - Sub2API config SHA256:
    `c53efa126074d361ba1928b2fbc42c752313afaf3276a0661d00a2873bb9143b`.
  - CLIProxy config SHA256:
    `29efa1e2c20ae0941a0a4ff30a8a8fd3c04c8311646d4948079496983e84d130`.
  - CLIProxy remains on `cliproxy-hysteria-internal` at
    `192.168.112.10`, with three plugin files.
  - Sub2API account `2` remains an active OpenAI API-key account with
    `force_responses`, Responses support and API-key WS v2 enabled, while
    `openai_passthrough` and `codex_fingerprint_mode` are absent.
- No build, remote candidate creation, Fork push, live database write, config
  edit, container restart, or routing change has been performed in this task
  yet.
- Local Sub2API validation at `4f29a58d` passed:
  - `go test ./... -count=1`
  - `go test -tags=unit ./... -count=1`
  - `go build ./...`
  - `corepack pnpm@9.15.9 install --frozen-lockfile`
  - `corepack pnpm@9.15.9 run test:run`
    (`223` files, `1550` tests)
  - `corepack pnpm@9.15.9 run build`
  - `git diff --check`
- Frontend test/build emitted existing environment, browserslist, dynamic-import,
  and chunk-size warnings but completed successfully and left no generated
  tracked-file changes.
- The upgraded source exposed a Docker build mismatch:
  `backend/go.mod` requires Go `1.26.6`, while the root Dockerfile still used
  `golang:1.26.5-alpine`. The Dockerfile was corrected and committed as
  `f685ccff94f9bd922dbde80b5ebe7e5daf727f57`.

## Stop Conditions

- Stop before integration if the worktree contains unrelated changes.
- Stop before remote startup if any candidate mount, network, IP, port,
  database, Redis, auth, or certificate path overlaps production.
- Stop if a candidate would share the live OAuth auth directory while the live
  CLIProxy remains active.
- Stop and remove candidate resources if either live container ID, image ID,
  restart count, start time, config hash, network membership, or account state
  changes during isolated testing.
- Treat any failed or unparsed acceptance assertion as incomplete.

## Next Verification

The isolated verification is complete. A future production rollout, live
configuration change, image swap, production canary, push, or PR requires
separate explicit authorization.

## Final Verification

- Final Sub2API branch and HEAD:
  - branch: `codex/update-upstream-20260815`
  - tracking branch: `fork/codex/update-upstream-20260815`
  - HEAD: `f685ccff94f9bd922dbde80b5ebe7e5daf727f57`
  - no push was performed
- Final Linux/amd64 candidate image IDs:
  - Sub2API:
    `sha256:8a0e88210c7a6d6f94e164fce3c259bea8cca930fe162245c8f30a6364e953ba`
  - CLIProxy production candidate:
    `sha256:e0e3ef59e58977954547960284a114ec976ff447b2e64bfba069613396d7c4f7`
  - CLIProxy isolated harness:
    `sha256:8038decd7b6d0f0d28a9f728fcdfa4ad90d3232f97fb9c0160073b78b66ca608`
- Exported artifact SHA256:
  - `sub2api-linux-amd64`:
    `cd03f8d28f9b09eb6562b00cfbe4126cd297cf62b11ec0851582e8c32a4d52bb`
  - `cliproxyapi-linux-amd64`:
    `d5a0e7b6e9b28869a2b4e8ab599152ce010a7b99b95cbb6a69d634f88c423d48`
  - `codex-fingerprint-convergence-linux-amd64.so`:
    `d05b190b8211444b373db7194f52fd9942d4eb37aa90a557561deb7f894deb20`
- The isolated Sub2API account persisted the intended ownership split:
  - `openai_passthrough=true`
  - `codex_fingerprint_mode=off`
  - Responses mode `force_responses`
  - API-key Responses WebSocket v2 mode `passthrough`
  - both the account and the test API key bound to `openai-default`
- Both CLIProxy plugins loaded and registered together on every measured
  startup:
  - `codex-fingerprint-convergence` version `0.2.0`
  - existing `codex-prompt-filter-go` version `0.1.0`
- The complete request matrix passed:
  - HTTP Responses non-streaming
  - HTTP/SSE Responses
  - native Responses WebSocket
  - two turns over one upstream WebSocket for macOS
  - two turns over one upstream WebSocket for Windows
  - two turns over one upstream WebSocket after Sub2API restart
  - 20 concurrent controlled requests
  - CLIProxy restart stability
  - Sub2API restart and database/config persistence
  - production-used model names:
    `gpt-5.6-sol`, `gpt-5.6-luna`, `gpt-5.6-terra`, and `gpt-5.5`
- Final sanitized capture assertions:
  - 36 measured final-upstream requests
  - exactly two stable installation profiles
  - macOS installation ID:
    `1c5c692a-d737-437b-a4de-7581bb5f9bf2`
  - Windows installation ID:
    `cbe7b5de-f19f-458e-85ce-33982566a1ec`
  - 36 distinct body session IDs
  - 36 distinct body thread IDs
  - 36 distinct prompt-cache keys
  - no failed assertions
- Candidate logs contained no `error`, `fatal`, or `panic` entries after
  filtering. Both plugin load/registration records were present.
- The live zero-change check passed before cleanup and again after cleanup:
  container IDs, image IDs, start times, restart counts, config hashes,
  CLIProxy network/IP, live plugin file count, and account `2` fields matched
  the pre-test baseline.
- Cleanup was verified:
  - all four candidate containers removed
  - candidate network removed
  - both candidate volumes removed
  - all three candidate images removed
  - temporary sources, database, Redis state, certificates, credentials,
    configs, logs, and remote scripts removed
- No live database backup was created, as requested.
- Local retained artifacts and sanitized evidence:
  `/Users/liusihang/Documents/ChatGPT/Sub2ApiArtifacts/2026-08-16-fingerprint-preprod-f685ccff`

## Status

Complete. The upgraded branch and the CLIProxy final-outbound fingerprint
plugin passed the isolated acceptance matrix without changing production.
