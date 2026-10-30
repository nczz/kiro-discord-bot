import assert from "node:assert/strict";
import test from "node:test";

import { activeAgentLifecycle, classifyAgentLifecycle } from "../dist/assets/agent-lifecycle.js";

test("agent lifecycle distinguishes active and terminal states", () => {
  assert.equal(classifyAgentLifecycle("queued"), "queued");
  assert.equal(classifyAgentLifecycle(" RUNNING "), "running");
  assert.equal(classifyAgentLifecycle("complete"), "complete");
  assert.equal(classifyAgentLifecycle("interrupted"), "interrupted");
  assert.equal(classifyAgentLifecycle("error"), "error");
  assert.equal(classifyAgentLifecycle("future-status"), "unknown");

  assert.equal(activeAgentLifecycle("queued"), "queued");
  assert.equal(activeAgentLifecycle("running"), "running");
  assert.equal(activeAgentLifecycle("complete"), undefined);
  assert.equal(activeAgentLifecycle("interrupted"), undefined);
  assert.equal(activeAgentLifecycle("error"), undefined);
  assert.equal(activeAgentLifecycle("unknown"), undefined);
});
