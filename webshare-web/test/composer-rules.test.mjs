import assert from "node:assert/strict";
import test from "node:test";

import {
  allowedMentionSelectionForDraft,
  commandName,
  highRiskCommand,
  mentionPreviewNames,
  resolveDraftMode,
  webshareCommandAllowed,
} from "../dist/assets/composer.js";
import { parseDiscordMessageReference, suggestedThreadName } from "../dist/assets/threads.js";

const picker = (overrides = {}) => ({
  users: [{ id: "user-2", displayName: "Bob", username: "bob" }],
  bot: { id: "bot-1", displayName: "Kiro" },
  selectedUsers: new Set(),
  botSelected: false,
  ...overrides,
});

test("slash selects command route but bot mention does not select agent route", () => {
  assert.equal(resolveDraftMode("message", "hello <@bot-1>"), "message");
  assert.equal(resolveDraftMode("agent", "<@bot-1> help"), "agent");
  assert.equal(resolveDraftMode("message", " /status"), "command");
  assert.equal(resolveDraftMode("command", "plain text"), "command");
});

test("command names and WebShare command availability are classified explicitly", () => {
  assert.equal(commandName("restart now"), "restart");
  assert.equal(commandName("  STATUS  "), "status");
  assert.equal(highRiskCommand("restart"), true);
  assert.equal(highRiskCommand("status"), false);
  assert.equal(webshareCommandAllowed("usage-history"), true);
  assert.equal(webshareCommandAllowed("status"), false);
  assert.equal(webshareCommandAllowed("cwd"), false);
  assert.equal(webshareCommandAllowed("session"), false);
  assert.equal(webshareCommandAllowed("doctor"), false);
  assert.equal(webshareCommandAllowed("start"), false);
  assert.equal(webshareCommandAllowed("resume"), false);
  assert.equal(webshareCommandAllowed("mcp"), false);
});

test("allowed mentions require both selected permission and an actual draft token", () => {
  const state = picker({ selectedUsers: new Set(["user-2"]), botSelected: true });
  assert.deepEqual(allowedMentionSelectionForDraft("hello", state, true, true), { users: [] });
  assert.deepEqual(allowedMentionSelectionForDraft("<@user-2> hello", state, true, true), { users: ["user-2"] });
  assert.deepEqual(allowedMentionSelectionForDraft("<@bot-1> hello", state, true, true), { users: [], bot: true });
  assert.deepEqual(allowedMentionSelectionForDraft("@Kiro hello", state, true, true), { users: [] });
  assert.deepEqual(allowedMentionSelectionForDraft("<@user-2> <@bot-1>", state, false, false), { users: [] });
});

test("mention preview names match the exact mentions that can ping", () => {
  const state = picker({ selectedUsers: new Set(["user-2"]), botSelected: true });
  assert.deepEqual(mentionPreviewNames("<@user-2> hi", state, true, true), ["@Bob"]);
  assert.deepEqual(mentionPreviewNames("<@bot-1> hi", state, true, true), ["@Kiro"]);
  assert.deepEqual(mentionPreviewNames("@Kiro hi", state, true, true), []);
  assert.deepEqual(mentionPreviewNames("<@user-2> <@bot-1>", state, true, true), ["@Kiro", "@Bob"]);
  assert.deepEqual(mentionPreviewNames("<@user-2> <@bot-1>", state, true, false), ["@Bob"]);
});

test("thread source accepts Discord message links or snowflake IDs", () => {
  assert.equal(parseDiscordMessageReference("123456789012345678"), "123456789012345678");
  assert.equal(parseDiscordMessageReference("https://discord.com/channels/1/2/123456789012345678"), "123456789012345678");
  assert.equal(parseDiscordMessageReference("https://canary.discord.com/channels/1/2/123456789012345678?jump=1"), "123456789012345678");
  assert.equal(parseDiscordMessageReference("not-a-message"), undefined);
});

test("thread names can be suggested from visible message content", () => {
  assert.equal(suggestedThreadName("Bob", "  outage   notes  "), "outage notes");
  assert.equal(suggestedThreadName("Bob", ""), "Thread with Bob");
});
