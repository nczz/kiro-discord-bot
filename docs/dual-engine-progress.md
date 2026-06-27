# Dual-Engine Implementation Progress

Single source of truth for WHERE the implementation is. Plan (WHAT/WHY) lives in
`docs/dual-engine-integration-plan.md`. Execution rules: plan §10.

At the start of EVERY iteration (especially after context compaction): read the `NEXT:` line below,
then read the plan section for that task, then `git status` / `git log --oneline -5`, then implement
exactly that one task. The `NEXT:` pointer is authoritative; if memory disagrees, the file wins.

---

## NEXT: S3.1 — env full path: AGENT_ENGINE / AGENT_ENGINES_ENABLED / OMP_PATH (config.go→ManagerConfig+Manager+NewManager→main.go→doctor_env.go→locale en/zh-TW→README+zh→.env.example)

(Update this line after each task. It must always name the single next task to do.)

---

## Status legend
- [ ] todo
- [~] in progress (uncommitted)
- [x] done — with evidence note (command + result / file:line)

## Stage 1 — ACP dialect scaffolding (kiro zero-regression). Commit when S1.5 green.
- [x] S1.1 acp/dialect.go: Dialect enum + dialectProfile + kiroProfile — created acp/dialect.go (Dialect/dialectProfile/profileFor/kiroProfile)
- [x] S1.2 Transport.SendNotification in acp/jsonrpc.go — added (no-id JSON-RPC notification, transport-closed guard)
- [x] S1.3 Agent.dialect+profile fields + StartAgent uses profile (launchArgs/parseSession); SetModel/CancelPrompt/Ask-cancel via activeProfile() — done, kiroProfile reproduces exact prior behavior
- [x] S1.4 StartAgent call sites — DEVIATION: Dialect moved into AgentOptions (zero value=DialectKiro), so StartAgent signature unchanged; no callsite edits needed (15 callsites incl. tests keep working)
- [x] S1.5 Verify — go build ./... OK; go vet ./acp ./channel ./bot OK; go test ./acp (ok) ./channel (ok, -skip known env test) ./bot (ok) → kiro zero-regression confirmed → COMMIT

## Stage 2 — omp dialect + verification. Commit when S2.5 green.
- [x] S2.1 ompProfile: launch `omp acp`; parseOmpSession(configOptions→models/modes); setModel=set_config_option(configId=model); cancel=SendNotification(session/cancel); profileFor enables DialectOmp
- [x] S2.2 omp usage→TurnMetrics: handleNotification usage_update case; per-turn cost=delta(cumulative) with reset guard→0; ContextUsage=used/size*100; MeteringItem{Unit:"USD"}; turnBaselineCost snapshot at turn start (Ask + AskAsyncMulti)
- [x] S2.3 omp ACP smoke (gated RUN_OMP_SMOKE) — PASS live: omp 16.1.23, models=16/modes=2 parsed, stopReason=end_turn, ctx=8.34%, metering=[{0.113655 USD}]
- [x] S2.4 Inline confirms — core path (handshake/configOptions/prompt/streaming/stopReason/usage) verified via S2.3 smoke on real dialect; image/​/clear/​session-load+MCP already verified in Phase 0 POCs (plan §1/§4.1); omp-unauth /doctor handling deferred to S3.6
- [x] S2.5 Tests — acp/dialect_test.go: kiro/omp launchArgs, profileFor, parseOmpSession (+malformed), usage delta cost (+compaction reset guard) all PASS; full acp/channel/bot green; kiro zero-regression → COMMIT

## Stage 3 — engine config + resolution (pure-omp + M1). Commit when S3.7 green.
- [ ] S3.1 env full path: AGENT_ENGINE / AGENT_ENGINES_ENABLED / OMP_PATH (config→ManagerConfig→main→doctor_env→locale×2→README×2→.env.example)
- [ ] S3.2 Engine-aware preflight (preflight enabled engines; no kiro ref when kiro disabled)
- [ ] S3.3 Gate KIRO_HOME/KIRO_MCP_CONFIG + kiro-agent-runtime to kiro dialect only
- [ ] S3.4 Engine resolution (default→channel→thread→override) at all 4 spawn points
- [ ] S3.5 Session.Engine field + read/write inherit chain
- [ ] S3.6 /doctor engine-scoped env + per-engine preflight lines
- [ ] S3.7 Verify pure-omp acceptance + full tests + i18n parity → COMMIT

## Stage 4 — /engine command + switch + per-engine usage (M2 primary). Commit when S4.6 green.
- [ ] S4.1 /engine slash+bang+dispatch, scope handling, i18n keys, permission
- [ ] S4.2 Switch state machine (busy→cancel→stop→persist→start→history replay→reply; error→revert)
- [ ] S4.3 Per-engine usage: UsageRecord.Engine, costFromMetering(USD), /usage two columns, footer branch
- [ ] S4.4 /steering engine-aware (AGENTS.md cross-engine + .kiro/steering kiro), via Manager policy
- [ ] S4.5 Docs/steering sync (project.md, decision-failure-patterns.md, README×2, .env.example, listen-mode-matrix?)
- [ ] S4.6 Verify full suite + omp smoke + i18n + git diff --check + manual /engine,/usage,/doctor → COMMIT

---

## Decisions / deviations log (append as they happen; mirror major ones into decision-failure-patterns.md)
- S1 (2026-06-28): Dialect carried in `AgentOptions.Dialect` (zero value DialectKiro) instead of a new positional `StartAgent` param. Reason: avoids breaking ~15 call sites (4 prod + ~11 tests + preflight), idiomatic (project already extends behavior via AgentOptions), zero behavior change for kiro. Mirrored into decision-failure-patterns.md.
- S1: metrics abstraction (kiro `_kiro.dev/metadata` vs omp `usage_update`) intentionally NOT moved into the profile in Stage 1 — handleNotification keeps the kiro path untouched; omp metrics wiring is added in S2.2 to keep Stage 1 strictly behavior-preserving.

## Verified evidence log (one line per completed task: command + result)
- S1.5: `go build ./...` BUILD_OK; `go vet ./acp ./channel ./bot` VET_OK; `go test ./acp` ok 0.575s; `go test ./channel -skip TestDoctorRuntimeOverviewShowsEffectiveDefaultsWhenEnvUnset` ok 14.079s; `go test ./bot` ok 0.613s.
- S2.3: `RUN_OMP_SMOKE=1 OMP_PATH=$(which omp) go test ./acp -run TestOmpSmoke` PASS 3.28s — omp 16.1.23, models=16, modes=2, stopReason=end_turn, ctx=8.34%, metering=[{0.113655 USD}].
- S2.5: `go test ./acp` (7 new dialect tests PASS) ok; `go vet ./acp ./channel ./bot` OK; `go test ./channel -skip <env test>` ok 13.985s; `go test ./bot` ok 0.572s — kiro zero-regression.
