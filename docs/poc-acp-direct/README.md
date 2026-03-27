# PoC: Direct ACP over stdio

Validated on 2026-03-27 with kiro-cli 1.28.1.

## Files
- `jsonrpc.go` — JSON-RPC 2.0 ndjson transport
- `agent.go` — ACP agent lifecycle (spawn, handshake, ask, cancel, stop)
- `main.go` — Test runner (6 tests)

## Run
```bash
cd docs/poc-acp-direct
go mod init acp-poc
go build -o acp-poc .
KIRO_CLI_PATH=/path/to/kiro-cli TEST_CWD=/tmp ./acp-poc
```

## Results
```
TEST 1: Start Agent     — PASS (pid, session obtained)
TEST 2: Simple Ask      — PASS (13 bytes, 1 chunk)
TEST 3: Streaming       — PASS (3 chunks)
TEST 4: Cancel          — PASS (context deadline exceeded)
TEST 5: Ask After Cancel — FAIL (kiro-cli Internal error — known limitation)
TEST 6: Stop Agent      — PASS (process gone)
```

Test 5 failure is a kiro-cli limitation, not a PoC bug. Cancel makes the session
unrecoverable. The bot handles this via health check + auto-restart (same as current
behavior through acp-bridge).
