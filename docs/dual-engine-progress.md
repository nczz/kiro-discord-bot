# Dual-Engine Implementation Progress

Single source of truth for WHERE the implementation is. Plan (WHAT/WHY) lives in
`docs/dual-engine-integration-plan.md`. Execution rules: plan §10.

At the start of EVERY iteration (especially after context compaction): read the `NEXT:` line below,
then read the plan section for that task, then `git status` / `git log --oneline -5`, then implement
exactly that one task. The `NEXT:` pointer is authoritative; if memory disagrees, the file wins.

---

## NEXT: S1.1 — Add acp/dialect.go (Dialect enum + dialectProfile + kiroProfile = current behavior)

(Update this line after each task. It must always name the single next task to do.)

---

## Status legend
- [ ] todo
- [~] in progress (uncommitted)
- [x] done — with evidence note (command + result / file:line)

## Stage 1 — ACP dialect scaffolding (kiro zero-regression). Commit when S1.5 green.
- [ ] S1.1 acp/dialect.go: Dialect enum + dialectProfile + kiroProfile
- [ ] S1.2 Transport.SendNotification in acp/jsonrpc.go
- [ ] S1.3 Agent.dialect field + StartAgent(dialect) + route launch/setModel/cancel/parse/metrics via profile (kiro identical)
- [ ] S1.4 Update all StartAgent call sites to DialectKiro (manager.go ×4 + preflight)
- [ ] S1.5 Verify build/vet/test; kiro behavior-identical → COMMIT

## Stage 2 — omp dialect + verification. Commit when S2.5 green.
- [ ] S2.1 ompProfile: launch `omp acp`; parse configOptions→models/modes; setModel=set_config_option(configId=model); cancel=SendNotification(session/cancel); metrics from usage_update/prompt-usage
- [ ] S2.2 omp usage→TurnMetrics: per-turn cost = delta cumulative (guard reset→0); ContextUsage=used/size*100; MeteringItem{Unit:"USD"}
- [ ] S2.3 omp ACP smoke (gated like RUN_ACP_SMOKE)
- [ ] S2.4 Inline-confirm: image e2e, /clear semantics, session/load+MCP, omp-unauth behavior
- [ ] S2.5 Tests: configOptions parse, omp delta cost (+compaction guard), cancel notification, kiro regression → COMMIT

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
- (none yet)

## Verified evidence log (one line per completed task: command + result)
- (none yet)
