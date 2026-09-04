import assert from "node:assert/strict";
import test from "node:test";

import {
  allowedMentionSelectionForDraft,
  commandName,
  displayDiscordMentions,
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

test("slash selects command route and bot mention selects agent route", () => {
  const { bot } = picker();
  assert.equal(resolveDraftMode("message", "hello <@bot-1>", bot), "agent");
  assert.equal(resolveDraftMode("message", "hello <@!bot-1>", bot), "agent");
  assert.equal(resolveDraftMode("agent", "<@bot-1> help", bot), "agent");
  assert.equal(resolveDraftMode("message", " /status", bot), "command");
  assert.equal(resolveDraftMode("command", "plain text", bot), "command");
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

test("display mentions renders raw and structured mentions as names", () => {
  const state = picker({
    users: [{ id: "123456789012345678", displayName: "mxp.tw", username: "mxp" }],
    bot: { id: "999999999999999999", displayName: "KDB" },
  });
  assert.equal(displayDiscordMentions("[[discord:user:123456789012345678]] hi <@999999999999999999>", [], state), "@mxp.tw hi @KDB");
  assert.equal(displayDiscordMentions("<@\u200b111111111111111111> <#222222222222222222> <@&333333333333333333>", [], state), "@Discord user #channel @role");
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
