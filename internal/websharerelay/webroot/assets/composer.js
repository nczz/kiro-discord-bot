import { el } from "./dom.js";
import { hasCapability } from "./protocol.js";
import { t } from "./i18n.js";
export function createComposer(locale, state, capabilities, getMentions, getAttachments, dispatch) {
    const root = el("div", { className: "card stack" });
    root.append(el("h2", { text: t(locale, "composer") }));
    const tabs = el("div", { className: "tabs" });
    const modes = [
        ["agent", t(locale, "agentPrompt"), hasCapability(capabilities, "sendAgentPrompt")],
        ["message", t(locale, "channelMessage"), hasCapability(capabilities, "postChannelMessage")],
        ["command", t(locale, "botCommand"), hasCapability(capabilities, "runBotCommand")],
    ];
    for (const [mode, label, enabled] of modes) {
        const button = el("button", { text: label });
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
        const command = el("input", { attrs: { placeholder: t(locale, "command") } });
        command.value = state.command;
        command.addEventListener("input", () => { state.command = command.value; });
        const args = el("textarea", { attrs: { placeholder: t(locale, "commandArgs") } });
        args.value = state.commandArgs;
        args.addEventListener("input", () => { state.commandArgs = args.value; });
        root.append(label(t(locale, "command"), command));
        root.append(label(t(locale, "commandArgs"), args));
    }
    else {
        const text = el("textarea", { attrs: { placeholder: t(locale, "messageText") } });
        text.value = state.text;
        text.addEventListener("input", () => { state.text = text.value; });
        root.append(label(t(locale, "messageText"), text));
    }
    const actions = el("div", { className: "composer-actions" });
    const send = el("button", { text: t(locale, "send") });
    send.addEventListener("click", () => sendCurrent(state, getMentions(), getAttachments(), dispatch));
    const interrupt = el("button", { text: t(locale, "interruptAgent") });
    interrupt.disabled = !hasCapability(capabilities, "interruptAgent");
    interrupt.addEventListener("click", () => dispatch({ type: "interrupt_agent" }));
    actions.append(send, interrupt);
    root.append(actions);
    return root;
}
function sendCurrent(state, mentions, attachments, dispatch) {
    const targetThreadID = state.targetThreadID || undefined;
    if (state.mode === "agent") {
        const text = state.text.trim();
        if (!text)
            return;
        dispatch({ type: "send_agent_prompt", text, attachments, ...(targetThreadID ? { targetThreadID } : {}) });
        state.text = "";
        return;
    }
    if (state.mode === "message") {
        const text = state.text.trim();
        if (!text)
            return;
        dispatch({ type: "post_channel_message", text, attachments, allowedMentions: mentions, ...(targetThreadID ? { targetThreadID } : {}) });
        state.text = "";
        return;
    }
    const command = state.command.trim();
    if (!command)
        return;
    dispatch({ type: "run_bot_command", command, args: parseArgs(state.commandArgs), ...(targetThreadID ? { targetThreadID } : {}) });
    state.command = "";
    state.commandArgs = "{}";
}
function parseArgs(input) {
    const trimmed = input.trim();
    if (!trimmed)
        return {};
    const parsed = JSON.parse(trimmed);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed))
        throw new Error("command_args_must_be_object");
    return parsed;
}
function label(text, child) {
    const node = el("label");
    node.append(document.createTextNode(text), child);
    return node;
}
