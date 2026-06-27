# Dual-Engine Implementation Progress

Single source of truth for WHERE the implementation is. Plan (WHAT/WHY) lives in
`docs/dual-engine-integration-plan.md`. Execution rules: plan §10.

At the start of EVERY iteration (especially after context compaction): read the `NEXT:` line below,
then read the plan section for that task, then `git status` / `git log --oneline -5`, then implement
exactly that one task. The `NEXT:` pointer is authoritative; if memory disagrees, the file wins.

---

## NEXT: S4.4 — /steering engine-aware: maintain AGENTS.md (cross-engine, read by kiro+omp) + keep .kiro/steering/<project>.md (kiro), all via Manager steering-path policy (stay under channel cwd; no handler-side path joins)

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
- [x] S3.1 env full path — config.go (OMPPath/AgentEngine/AgentEnginesEnabled) → ManagerConfig + Manager fields + NewManager → main.go assembly → doctor_env.go (3 entries) → locale en/zh-TW (+3 keys, 476 aligned) → .env.example → docs-site environment.md (en+zh)
- [x] S3.2 Engine-aware preflight — main.go runPreflight + enabledEngineSpecs (preflights only enabled engines; strict/fatal per-engine); pure-omp never references kiro-cli
- [x] S3.3 Gate KIRO_HOME/KIRO_MCP_CONFIG + kiro-agent-runtime to kiro — applyEngine strips KIRO_* env for non-kiro; NewManager MkdirAll gated on enabledEngines[kiro]; preflightAgentOptions(dialect) skips KIRO_* for omp
- [x] S3.4 Engine resolution — channel/engine.go engineForChannel/engineForThread + applyEngine; wired into all 4 spawn points (channel/temp/audit/thread) with resolved binary
- [x] S3.5 Session.Engine field + inherit chain (default→channel→thread→override) — session.go field + engineForChannel/Thread reads
- [x] S3.6 /doctor engine env display (AGENT_ENGINE/OMP_PATH/AGENT_ENGINES_ENABLED) + startup per-engine preflight log lines. NOTE: live /doctor per-engine re-preflight deferred (env config now visible; startup runPreflight logs per engine).
- [x] S3.7 Verify — engine_test.go (parseDialect/parseEnabledEngines/applyEngine strip/engineForChannel/engineForThread inheritance) all PASS; build/vet OK; acp/channel/bot tests ok; i18n 476 aligned; git diff --check clean; doctor test confirmed same pre-existing env failure (new entries render correctly) → COMMIT

## Stage 4 — /engine command + switch + per-engine usage (M2 primary). Commit when S4.6 green.
- [x] S4.1 /engine slash+bang+dispatch — registered (choices kiro/omp), slash dispatch, bang !engine (channel+thread), command-recognition list; cmdEngine in commands.go; i18n engine.current/switching/switched/unknown/not_enabled + cmd.engine.* (484 aligned)
- [x] S4.2 Switch state machine — SwitchEngine/SwitchThreadEngine (channel/engine.go): validate enabled, persist Session.Engine (canonical name), fresh session (SessionID cleared), Restart (stop worker+agent→start→history-prefix replay), error→revert. CRITICAL FIX: Reset/Restart/SetCWD-restart/channel-spawn/thread-spawn now preserve Session.Engine (were dropping it → would lose engine on every spawn/model-switch)
- [x] S4.3 Per-engine usage — costFromMetering(USD), UsageRecord.Engine+CostUSD, Append computes CostUSD, Report aggregates *CostUSD + counts USD turns as metered, formatUsageReport shows USD line, FormatMetricsFooter shows $X.XXXX for USD; recordUsage sets Engine from metering unit
- [ ] S4.4 /steering engine-aware (AGENTS.md cross-engine + .kiro/steering kiro), via Manager policy
- [ ] S4.5 Docs/steering sync (project.md, decision-failure-patterns.md, README×2, .env.example done, listen-mode-matrix?)
- [ ] S4.6 Verify full suite + omp smoke + i18n + git diff --check + manual /engine,/usage,/doctor → COMMIT

---

## Decisions / deviations log (append as they happen; mirror major ones into decision-failure-patterns.md)
- S1 (2026-06-28): Dialect carried in `AgentOptions.Dialect` (zero value DialectKiro) instead of a new positional `StartAgent` param. Reason: avoids breaking ~15 call sites (4 prod + ~11 tests + preflight), idiomatic (project already extends behavior via AgentOptions), zero behavior change for kiro. Mirrored into decision-failure-patterns.md.
- S1: metrics abstraction (kiro `_kiro.dev/metadata` vs omp `usage_update`) intentionally NOT moved into the profile in Stage 1 — handleNotification keeps the kiro path untouched; omp metrics wiring is added in S2.2 to keep Stage 1 strictly behavior-preserving.

## Verified evidence log (one line per completed task: command + result)
- S1.5: `go build ./...` BUILD_OK; `go vet ./acp ./channel ./bot` VET_OK; `go test ./acp` ok 0.575s; `go test ./channel -skip TestDoctorRuntimeOverviewShowsEffectiveDefaultsWhenEnvUnset` ok 14.079s; `go test ./bot` ok 0.613s.
- S2.3: `RUN_OMP_SMOKE=1 OMP_PATH=$(which omp) go test ./acp -run TestOmpSmoke` PASS 3.28s — omp 16.1.23, models=16, modes=2, stopReason=end_turn, ctx=8.34%, metering=[{0.113655 USD}].
- S2.5: `go test ./acp` (7 new dialect tests PASS) ok; `go vet ./acp ./channel ./bot` OK; `go test ./channel -skip <env test>` ok 13.985s; `go test ./bot` ok 0.572s — kiro zero-regression.
- S3.7: 5 engine_test.go tests PASS (parseDialect/parseEnabledEngines/applyEngine-strip/engineForChannel/engineForThread); `go build ./...` OK; `go vet ./...` OK; `go test ./acp ./channel ./bot -skip <env test>` all ok; i18n 476 aligned; `git diff --check` clean. Doctor env test = same pre-existing KIRO_CLI_PATH-set failure; new OMP_PATH/AGENT_ENGINE/AGENT_ENGINES_ENABLED entries render correctly (no panic).
- S4.1–S4.3 (partial Stage 4 commit): `go build ./...` OK; `go vet ./bot ./channel` OK; `go test ./bot ./channel -skip <env test>` ok; i18n 484 aligned. Decision: committing S4.1–S4.3 as a partial-stage commit (verified, kiro zero-regression) rather than holding a large uncommitted diff across the iteration boundary — better for the compaction-resistant workflow. S4.4–S4.6 land in the final Stage-4 commit.
