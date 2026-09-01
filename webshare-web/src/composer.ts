import { el } from "./dom.js";
import type { AttachmentRef, ClientAction, Capabilities } from "./protocol.js";
import { hasCapability } from "./protocol.js";
import type { AllowedMentionSelection } from "./protocol.js";
import type { Locale } from "./i18n.js";
import { t } from "./i18n.js";

export type ComposerMode = "agent" | "message" | "command";

export interface ComposerState {
  mode: ComposerMode;
  text: string;
  command: string;
  commandArgs: string;
  targetThreadID: string | undefined;
}

export function createComposer(
  locale: Locale,
  state: ComposerState,
  capabilities: Capabilities | undefined,
  getMentions: () => AllowedMentionSelection,
  getAttachments: () => AttachmentRef[],
  dispatch: (action: ClientAction) => void,
): HTMLElement {
  const root = el("div", { className: "card stack" });
  root.append(el("h2", { text: t(locale, "composer") }));

  const tabs = el("div", { className: "tabs" });
  const modes: Array<[ComposerMode, string, boolean]> = [
    ["agent", t(locale, "agentPrompt"), hasCapability(capabilities, "sendAgentPrompt")],
    ["message", t(locale, "channelMessage"), hasCapability(capabilities, "postChannelMessage")],
    ["command", t(locale, "botCommand"), hasCapability(capabilities, "runBotCommand")],
  ];
  for (const [mode, label, enabled] of modes) {
    const button = el("button", { text: label }) as HTMLButtonElement;
    button.classList.toggle("active", state.mode === mode);
    button.disabled = !enabled;
    button.addEventListener("click", () => {
      state.mode = mode;
      root.dispatchEvent(new CustomEvent("composer-change", { bubbles: true }));
    });
    tabs.append(button);
  }
  root.append(tabs);

  if (state.mode === "command") {
    const command = el("input", { attrs: { placeholder: t(locale, "command") } }) as HTMLInputElement;
    command.value = state.command;
    command.addEventListener("input", () => { state.command = command.value; });
    const args = el("textarea", { attrs: { placeholder: t(locale, "commandArgs") } }) as HTMLTextAreaElement;
    args.value = state.commandArgs;
    args.addEventListener("input", () => { state.commandArgs = args.value; });
    root.append(label(t(locale, "command"), command));
    root.append(label(t(locale, "commandArgs"), args));
  } else {
    const text = el("textarea", { attrs: { placeholder: t(locale, "messageText") } }) as HTMLTextAreaElement;
    text.value = state.text;
    text.addEventListener("input", () => { state.text = text.value; });
    root.append(label(t(locale, "messageText"), text));
  }

  const actions = el("div", { className: "composer-actions" });
  const send = el("button", { text: t(locale, "send") }) as HTMLButtonElement;
  send.addEventListener("click", () => sendCurrent(state, getMentions(), getAttachments(), dispatch));
  const interrupt = el("button", { text: t(locale, "interruptAgent") }) as HTMLButtonElement;
  interrupt.disabled = !hasCapability(capabilities, "interruptAgent");
  interrupt.addEventListener("click", () => dispatch({ type: "interrupt_agent" }));
  actions.append(send, interrupt);
  root.append(actions);
  return root;
}

function sendCurrent(state: ComposerState, mentions: AllowedMentionSelection, attachments: AttachmentRef[], dispatch: (action: ClientAction) => void): void {
  const targetThreadID = state.targetThreadID || undefined;
  if (state.mode === "agent") {
    const text = state.text.trim();
    if (!text) return;
    dispatch({ type: "send_agent_prompt", text, attachments, ...(targetThreadID ? { targetThreadID } : {}) });
    state.text = "";
    return;
  }
  if (state.mode === "message") {
    const text = state.text.trim();
    if (!text) return;
    dispatch({ type: "post_channel_message", text, attachments, allowedMentions: mentions, ...(targetThreadID ? { targetThreadID } : {}) });
    state.text = "";
    return;
  }
  const command = state.command.trim();
  if (!command) return;
  dispatch({ type: "run_bot_command", command, args: parseArgs(state.commandArgs), ...(targetThreadID ? { targetThreadID } : {}) });
  state.command = "";
  state.commandArgs = "{}";
}

function parseArgs(input: string): Record<string, unknown> {
  const trimmed = input.trim();
  if (!trimmed) return {};
  const parsed = JSON.parse(trimmed) as unknown;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("command_args_must_be_object");
  return parsed as Record<string, unknown>;
}

function label(text: string, child: HTMLElement): HTMLLabelElement {
  const node = el("label");
  node.append(document.createTextNode(text), child);
  return node;
}
