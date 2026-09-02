import { continueAcceptedUpload, downloadURL, queueUploads, selectedAttachmentRefs } from "./attachments.js";
import { deriveSessionKeys, openJSON, parseJoinFragment, sealJSON, writeTokenProof } from "./crypto.js";
import { clear, el, formatBytes } from "./dom.js";
import { chooseLocale, setLocale, t } from "./i18n.js";
import { mentionSelection } from "./mentions.js";
import { hasCapability, PROTOCOL_VERSION } from "./protocol.js";
import { WebShareTransport } from "./transport.js";
const roomHistoryVersion = 1;
const roomHistoryLimit = 300;
const roomHistoryPrefix = "kdb-webshare:room:";
const commandSuggestions = [
    "agent",
    "audit",
    "back",
    "clear",
    "close-thread",
    "compact",
    "cwd",
    "doctor",
    "cron-list",
    "cron-run",
    "engine",
    "help",
    "flashmemory",
    "interrupt",
    "mcp",
    "memory",
    "close",
    "model",
    "models",
    "pause",
    "remind",
    "reset",
    "restart",
    "resume",
    "session",
    "skill",
    "status",
    "thread",
    "usage",
    "webhook",
    "webshare",
    "usage-history",
];
const state = {
    locale: chooseLocale(),
    join: undefined,
    keys: undefined,
    transport: undefined,
    status: "disconnected",
    statusDetail: undefined,
    sendSeq: 1,
    lastReceiveSeqByPeer: new Map(),
    hostPeerID: undefined,
    capabilities: undefined,
    target: undefined,
    opener: undefined,
    threads: [],
    mentionPicker: { users: [], bot: undefined, selectedUsers: new Set(), botSelected: false },
    draft: { mode: "agent", text: "", targetThreadID: undefined, newThreadName: "", sourceMessageID: "" },
    uploads: { pending: [], queued: [], inProgress: new Map(), downloads: new Map() },
    pendingAttachmentDownloads: new Set(),
    messages: [],
    error: undefined,
    seenMessageIDs: new Set(),
    clientPeerID: randomClientPeerID(),
};
function randomClientPeerID() {
    const bytes = new Uint32Array(1);
    do {
        crypto.getRandomValues(bytes);
    } while (bytes[0] === 0);
    return bytes[0] ?? 1;
}
const appRoot = document.querySelector("#app");
if (!appRoot)
    throw new Error("missing_app_root");
const app = appRoot;
installViewportHeightVar();
void bootstrap();
function installViewportHeightVar() {
    const root = document.documentElement;
    const update = () => {
        const viewport = window.visualViewport;
        const height = viewport?.height ?? window.innerHeight;
        root.style.setProperty("--app-height", `${Math.max(1, Math.round(height))}px`);
    };
    update();
    window.addEventListener("resize", update, { passive: true });
    window.addEventListener("orientationchange", update, { passive: true });
    window.visualViewport?.addEventListener("resize", update, { passive: true });
    window.visualViewport?.addEventListener("scroll", update, { passive: true });
}
async function bootstrap() {
    try {
        state.join = parseJoinFragment();
        restoreRoomHistory();
        state.keys = await deriveSessionKeys(state.join.roomID, state.join.roomKey);
        state.transport = new WebShareTransport({
            roomID: state.join.roomID,
            onStatus: (status, detail) => {
                state.status = status;
                state.statusDetail = detail;
                render();
                if (status === "connected")
                    void sendHello();
            },
            onFrame: (frame) => void handleFrame(frame),
            onError: (error) => {
                state.error = error instanceof Error ? error.message : String(error);
                render();
            },
        });
        state.transport.connect();
    }
    catch (error) {
        state.error = error instanceof Error ? error.message : String(error);
    }
    render();
}
async function sendHello() {
    if (!state.join)
        return;
    const hello = { type: "hello", proto: PROTOCOL_VERSION, displayName: t(state.locale, "browserDisplayName") };
    const token = state.join.writeToken ? writeTokenProof(state.join.writeToken) : undefined;
    if (token)
        hello.writeToken = token;
    await dispatch(hello);
}
async function dispatch(action) {
    if (!state.keys || !state.transport || !state.join)
        return;
    if (!state.join.canWrite && action.type !== "hello") {
        state.error = t(state.locale, "viewOnly");
        render();
        return;
    }
    try {
        const signedAction = actionWithWriteToken(action);
        const seq = state.sendSeq;
        state.sendSeq += 1;
        const payload = await sealJSON(signedAction, state.keys.guestToHost, state.join.roomID, state.clientPeerID, seq, 2, "guest_to_host");
        if (!state.transport.send(payload, state.hostPeerID ?? 0))
            state.error = t(state.locale, "notConnected");
    }
    catch (error) {
        state.error = error instanceof Error ? error.message : String(error);
    }
    render();
}
async function handleFrame(frame) {
    if (!state.keys || !state.join)
        return;
    try {
        const event = await openJSON(frame.payload, state.keys.hostToGuest, state.join.roomID, frame.peerID, 3, "host_to_guest");
        const envelope = JSON.parse(new TextDecoder().decode(frame.payload));
        if (typeof envelope.seq === "number") {
            const last = state.lastReceiveSeqByPeer.get(frame.peerID) ?? -1;
            if (envelope.seq <= last)
                throw new Error("replayed_frame");
            state.lastReceiveSeqByPeer.set(frame.peerID, envelope.seq);
        }
        state.hostPeerID = frame.peerID;
        applyServerEvent(event);
    }
    catch (error) {
        state.error = error instanceof Error ? error.message : String(error);
    }
    render();
}
function applyServerEvent(event) {
    switch (event.type) {
        case "welcome": {
            const previousSelectedThreadID = state.draft.targetThreadID;
            state.capabilities = event.capabilities;
            state.target = event.target;
            state.opener = event.opener;
            state.threads = event.threads ? [...event.threads] : [];
            if (event.target.threadID)
                upsertThread({ id: event.target.threadID, name: event.target.threadName ?? event.target.threadID, parentChannelID: event.target.channelID, selected: true });
            state.draft.targetThreadID = welcomeSelectedThread(event, previousSelectedThreadID);
            state.mentionPicker.users = event.mentionableUsers ?? [];
            state.mentionPicker.bot = event.mentionableBot;
            const welcomeText = `${event.opener.displayName} → ${targetLabel(event.target)}`;
            if (!state.messages.some((message) => message.kind === "system" && message.content === welcomeText))
                pushMessage("system", t(state.locale, "systemAuthor"), welcomeText);
            break;
        }
        case "channel_event":
            mergeMentionables(event.event.mentionableUsers, event.event.mentionableBot);
            applyDiscordMessage("discord", event.event, event.event.content ?? "", event.event.author?.displayName ?? "Discord");
            break;
        case "thread_event":
            applyThreadEvent(event.event);
            break;
        case "agent_event":
            pushMessage("agent", t(state.locale, "agentAuthor"), event.event.content ?? event.event.status, { timestamp: event.event.timestamp });
            break;
        case "command_result":
            pushMessage("command", t(state.locale, "botAuthor"), event.content || event.status);
            break;
        case "upload_state":
            handleUploadState(event);
            break;
        case "attachment_stream": {
            const existing = state.uploads.downloads.get(event.streamID);
            const chunks = existing ? [...existing.chunks] : [];
            if (event.chunk)
                chunks.push(event.chunk);
            const done = Boolean(event.done || existing?.done);
            state.uploads.downloads.set(event.streamID, { metadata: event.metadata, chunks, done });
            handleFetchedAttachment(event.metadata, done);
            break;
        }
        case "notice":
            pushMessage("system", t(state.locale, "systemAuthor"), event.messageKey);
            break;
        case "error":
            state.pendingAttachmentDownloads.clear();
            state.error = `${event.reasonCode ?? event.code ?? "error"}: ${event.content ?? event.messageKey ?? ""}`;
            pushMessage("error", t(state.locale, "systemAuthor"), state.error);
            break;
        case "bye":
            pushMessage("system", t(state.locale, "systemAuthor"), event.reasonCode);
            state.transport?.stop();
            break;
    }
    persistRoomHistory();
}
function handleUploadState(event) {
    if (event.status === "accepted") {
        pushMessage("upload", t(state.locale, "systemAuthor"), `${t(state.locale, "uploadAccepted")} · ${event.uploadID}`);
        void continueAcceptedUpload(state.uploads, event.uploadID, (action) => dispatch(action));
        return;
    }
    if (event.status === "complete" && event.metadata?.attachmentRef) {
        rememberUploadedAttachment(event.metadata);
        pushMessage("upload", t(state.locale, "systemAuthor"), `${t(state.locale, "uploadComplete")} · ${event.metadata.filename}`);
        return;
    }
    if (event.status === "rejected") {
        discardUpload(event.uploadID);
        pushMessage("error", t(state.locale, "systemAuthor"), event.reason ?? event.reasonCode ?? "upload rejected");
    }
}
function render() {
    const oldList = app.querySelector(".message-list");
    const shouldStickToBottom = !oldList || oldList.scrollHeight - oldList.scrollTop - oldList.clientHeight < 80;
    clear(app);
    app.append(discordShell(shouldStickToBottom));
}
function discordShell(shouldStickToBottom) {
    const shell = el("main", { className: "discord-app" });
    shell.append(serverRail(), channelSidebar(), chatPane(shouldStickToBottom), memberSidebar());
    return shell;
}
function serverRail() {
    const rail = el("aside", { className: "server-rail", attrs: { "aria-label": "WebShare" } });
    rail.append(el("div", { className: "server-icon active", text: "WS" }));
    return rail;
}
function channelSidebar() {
    const nav = el("nav", { className: "channel-sidebar", attrs: { "aria-label": t(state.locale, "channels") } });
    nav.append(el("div", { className: "guild-title", text: t(state.locale, "appTitle") }));
    nav.append(channelButton(undefined, targetChannelLabel(), !state.draft.targetThreadID));
    nav.append(el("div", { className: "sidebar-section", text: t(state.locale, "threads") }));
    for (const thread of state.threads)
        nav.append(channelButton(thread.id, thread.name, state.draft.targetThreadID === thread.id));
    nav.append(createThreadInline());
    return nav;
}
function channelButton(threadID, label, active) {
    const button = el("button", { className: active ? "channel-link active" : "channel-link", text: `# ${label}` });
    button.addEventListener("click", () => selectThread(threadID));
    return button;
}
function createThreadInline() {
    const box = el("div", { className: "thread-create" });
    const name = el("input", { attrs: { placeholder: t(state.locale, "threadName") } });
    const source = el("input", { attrs: { placeholder: t(state.locale, "sourceMessageCompact") } });
    const create = el("button", { text: t(state.locale, "createThread") });
    name.value = state.draft.newThreadName;
    source.value = state.draft.sourceMessageID;
    name.addEventListener("input", () => { state.draft.newThreadName = name.value; });
    source.addEventListener("input", () => { state.draft.sourceMessageID = source.value; });
    create.disabled = !canWrite("createThread");
    create.addEventListener("click", () => {
        const threadName = state.draft.newThreadName.trim();
        if (!threadName)
            return;
        const sourceMessageID = state.draft.sourceMessageID.trim();
        void dispatch({ type: "create_thread", name: threadName, ...(sourceMessageID ? { sourceMessageID } : {}) });
        state.draft.newThreadName = "";
        state.draft.sourceMessageID = "";
        render();
    });
    box.append(el("div", { className: "hint", text: t(state.locale, "threadCreateHint") }), name, source, create);
    return box;
}
function chatPane(shouldStickToBottom) {
    const pane = el("section", { className: "chat-pane" });
    pane.append(chatHeader());
    const list = el("div", { className: "message-list", attrs: { role: "log", "aria-live": "polite" } });
    const thread = selectedThread();
    list.append(delegationNotice());
    if (!state.join)
        list.append(systemBlock(t(state.locale, "invalidLink")));
    if (state.error)
        list.append(systemBlock(state.error, true));
    if (thread)
        list.append(threadContextPanel(thread));
    const visible = visibleMessages();
    if (visible.length === 0)
        list.append(systemBlock(thread ? t(state.locale, "noThreadMessages") : t(state.locale, "eventNone")));
    for (const message of visible)
        list.append(messageRow(message));
    pane.append(list, composerBar());
    if (shouldStickToBottom)
        requestAnimationFrame(() => { list.scrollTop = list.scrollHeight; });
    return pane;
}
function chatHeader() {
    const header = el("header", { className: "chat-header" });
    const thread = selectedThread();
    const title = el("div", {
        className: "chat-title",
        children: thread
            ? [el("span", { className: "hash", text: "#" }), el("span", { className: "breadcrumb", text: targetChannelLabel() }), el("span", { className: "breadcrumb-sep", text: "/" }), el("strong", { text: thread.name })]
            : [el("span", { className: "hash", text: "#" }), el("strong", { text: activeTargetLabel() })],
    });
    const locale = el("select", { className: "locale-select" });
    locale.append(new Option("English", "en"), new Option("繁體中文", "zh-TW"));
    locale.value = state.locale;
    locale.addEventListener("change", () => {
        state.locale = locale.value === "zh-TW" ? "zh-TW" : "en";
        setLocale(state.locale);
        render();
    });
    const actions = [statusPill(), locale];
    if (thread) {
        const back = el("button", { className: "back-button", text: t(state.locale, "backToChannel") });
        back.type = "button";
        back.addEventListener("click", () => selectThread(undefined));
        actions.unshift(back);
    }
    header.append(title, el("div", { className: "header-actions", children: actions }));
    return header;
}
function delegationNotice() {
    const mode = state.join?.canWrite ? t(state.locale, "writeLink") : t(state.locale, "viewOnly");
    return el("article", { className: "delegation-notice", children: [el("strong", { text: t(state.locale, "delegationWarningTitle") }), el("span", { text: `${mode} · ${t(state.locale, "delegationWarningBody")}` })] });
}
function composerBar() {
    const inferredMode = currentDraftMode();
    const disabled = !composerWritable();
    const wrap = el("form", { className: "composer-bar" });
    wrap.addEventListener("submit", (event) => {
        event.preventDefault();
        sendDraft();
    });
    const hint = el("div", { className: "composer-hint", text: composerHint(inferredMode) });
    const inputRow = el("div", { className: "composer-input-row" });
    const upload = uploadControl(disabled || inferredMode === "command" || !canWrite("upload"));
    const textarea = el("textarea", { attrs: { placeholder: composerPlaceholder(), rows: "1", autocomplete: "off", spellcheck: "true" } });
    const suggestions = el("div", { className: "composer-suggestions hidden", attrs: { role: "listbox" } });
    textarea.value = state.draft.text;
    textarea.disabled = disabled;
    const refreshComposer = () => {
        state.draft.text = textarea.value;
        const mode = currentDraftMode(textarea.value);
        hint.textContent = composerHint(mode);
        setUploadControlDisabled(upload, disabled || mode === "command" || !canWrite("upload"));
        renderComposerSuggestions(textarea, suggestions, refreshComposer);
    };
    let composing = false;
    textarea.addEventListener("compositionstart", () => {
        composing = true;
    });
    textarea.addEventListener("compositionend", () => {
        composing = false;
        refreshComposer();
    });
    textarea.addEventListener("input", refreshComposer);
    textarea.addEventListener("click", () => renderComposerSuggestions(textarea, suggestions, refreshComposer));
    textarea.addEventListener("keyup", () => renderComposerSuggestions(textarea, suggestions, refreshComposer));
    textarea.addEventListener("keydown", (event) => {
        if (event.key === "Tab" && acceptFirstComposerSuggestion(textarea)) {
            event.preventDefault();
            refreshComposer();
            return;
        }
        if (event.key === "Enter" && !event.shiftKey) {
            if (composing || event.isComposing || event.keyCode === 229)
                return;
            event.preventDefault();
            sendDraft();
        }
    });
    const send = el("button", { className: "send-button", text: t(state.locale, "send") });
    send.disabled = disabled;
    inputRow.append(upload, textarea, send);
    wrap.append(hint);
    if (inferredMode !== "command")
        wrap.append(attachmentChips());
    wrap.append(suggestions, inputRow, inlineUtilityRow());
    renderComposerSuggestions(textarea, suggestions, refreshComposer);
    return wrap;
}
function setUploadControlDisabled(control, disabled) {
    control.classList.toggle("disabled", disabled);
    const input = control.querySelector("input");
    if (input)
        input.disabled = disabled;
}
function activeComposerToken(text, caret) {
    const before = text.slice(0, caret);
    const leading = before.length - before.trimStart().length;
    const commandPart = before.slice(leading);
    if (commandPart.startsWith("/") && !/\s/.test(commandPart))
        return { kind: "command", start: leading, end: caret, query: commandPart.slice(1).toLowerCase() };
    const at = before.lastIndexOf("@");
    if (at < 0 || (at > 0 && !/\s/.test(before.charAt(at - 1))))
        return undefined;
    const query = before.slice(at + 1);
    if (/\s/.test(query))
        return undefined;
    return { kind: "mention", start: at, end: caret, query: query.toLowerCase() };
}
function composerSuggestions(textarea) {
    const token = activeComposerToken(textarea.value, textarea.selectionStart ?? textarea.value.length);
    if (!token)
        return [];
    if (token.kind === "command") {
        if (!canWrite("runBotCommand"))
            return [];
        return commandSuggestions
            .filter((command) => command.startsWith(token.query))
            .slice(0, 8)
            .map((command) => ({ kind: "command", id: command, label: `/${command}`, detail: t(state.locale, "botCommand"), insert: `/${command} ` }));
    }
    const out = [];
    if (state.mentionPicker.bot && canMention(true)) {
        const bot = state.mentionPicker.bot;
        const aliases = [bot.displayName, bot.id].map((item) => item.toLowerCase());
        if (token.query === "" || aliases.some((alias) => alias.includes(token.query)))
            out.push({ kind: "bot", id: bot.id, label: `@${bot.displayName}`, detail: t(state.locale, "botAuthor"), insert: `<@${bot.id}> ` });
    }
    if (canMention(false)) {
        for (const user of state.mentionPicker.users) {
            const aliases = [user.displayName, user.username ?? "", user.id].map((item) => item.toLowerCase());
            if (token.query !== "" && !aliases.some((alias) => alias.includes(token.query)))
                continue;
            out.push({ kind: "user", id: user.id, label: `@${user.displayName}`, detail: user.username ?? t(state.locale, "members"), insert: `<@${user.id}> ` });
            if (out.length >= 8)
                break;
        }
    }
    return out.slice(0, 8);
}
function renderComposerSuggestions(textarea, root, refresh) {
    clear(root);
    const suggestions = composerSuggestions(textarea);
    root.classList.toggle("hidden", suggestions.length === 0);
    for (const suggestion of suggestions) {
        const button = el("button", { className: "composer-suggestion", attrs: { type: "button", role: "option" }, children: [el("strong", { text: suggestion.label }), el("span", { text: suggestion.detail })] });
        button.addEventListener("mousedown", (event) => event.preventDefault());
        button.addEventListener("click", () => {
            acceptComposerSuggestion(textarea, suggestion);
            refresh();
            textarea.focus();
        });
        root.append(button);
    }
}
function acceptFirstComposerSuggestion(textarea) {
    const [first] = composerSuggestions(textarea);
    if (!first)
        return false;
    return acceptComposerSuggestion(textarea, first);
}
function acceptComposerSuggestion(textarea, suggestion) {
    const token = activeComposerToken(textarea.value, textarea.selectionStart ?? textarea.value.length);
    if (!token)
        return false;
    textarea.setRangeText(suggestion.insert, token.start, token.end, "end");
    if (suggestion.kind === "bot")
        state.mentionPicker.botSelected = true;
    if (suggestion.kind === "user")
        state.mentionPicker.selectedUsers.add(suggestion.id);
    state.draft.text = textarea.value;
    return true;
}
function uploadControl(disabled) {
    const label = el("label", { className: disabled ? "upload-icon disabled" : "upload-icon", text: "+" });
    const input = el("input", { attrs: { type: "file", multiple: "true" } });
    input.disabled = disabled;
    input.addEventListener("change", () => void queueUploads(input.files, state.uploads, (action) => dispatch(action)));
    label.append(input);
    return label;
}
function inlineUtilityRow() {
    const row = el("div", { className: "composer-utility" });
    row.append(inlineMentionFallback(), el("span", { className: "target-chip", text: `${t(state.locale, "writeAs")} ${state.opener?.displayName ?? "-"}` }));
    return row;
}
function attachmentChips() {
    const chips = el("div", { className: "attachment-strip" });
    for (const attachment of state.uploads.pending)
        chips.append(el("span", { className: "attachment-chip", text: `${attachment.filename} · ${formatBytes(attachment.size)}` }));
    return chips;
}
function inlineMentionFallback() {
    const wrap = el("div", { className: "mention-inline", attrs: { "aria-label": t(state.locale, "members") } });
    if (state.mentionPicker.bot)
        wrap.append(memberToggle(state.mentionPicker.bot.id, state.mentionPicker.bot.displayName, true));
    for (const user of state.mentionPicker.users)
        wrap.append(memberToggle(user.id, user.displayName, false));
    return wrap;
}
function memberSidebar() {
    const aside = el("aside", { className: "member-sidebar", attrs: { "aria-label": t(state.locale, "members") } });
    aside.append(el("div", { className: "member-title", text: t(state.locale, "members") }), el("p", { className: "member-help", text: t(state.locale, "memberHelp") }));
    if (state.mentionPicker.bot)
        aside.append(memberToggle(state.mentionPicker.bot.id, state.mentionPicker.bot.displayName, true));
    for (const user of state.mentionPicker.users)
        aside.append(memberToggle(user.id, user.displayName, false));
    aside.append(el("p", { className: "member-help", text: t(state.locale, "noMentionEveryone") }));
    return aside;
}
function memberToggle(id, name, isBot) {
    const label = el("label", { className: "member-row" });
    const checkbox = el("input", { attrs: { type: "checkbox" } });
    checkbox.checked = isBot ? state.mentionPicker.botSelected : state.mentionPicker.selectedUsers.has(id);
    checkbox.disabled = !canMention(isBot);
    checkbox.addEventListener("change", () => {
        if (isBot)
            state.mentionPicker.botSelected = checkbox.checked;
        else if (checkbox.checked)
            state.mentionPicker.selectedUsers.add(id);
        else
            state.mentionPicker.selectedUsers.delete(id);
        render();
    });
    label.append(checkbox, avatar(name, isBot ? "bot" : "user"), el("span", { text: isBot ? `${name} (bot)` : name }));
    return label;
}
function messageRow(message) {
    const row = el("article", { className: `message-row ${message.kind}${message.edited ? " edited" : ""}${message.deleted ? " deleted" : ""}` });
    const body = el("div", { className: "message-body" });
    const meta = [el("strong", { text: message.author }), el("span", { text: message.timestamp })];
    if (message.edited && !message.deleted)
        meta.push(el("span", { className: "message-edited", text: t(state.locale, "messageEdited") }));
    body.append(el("div", { className: "message-meta", children: meta }));
    if (message.replyTo)
        body.append(replyPreview(message.replyTo));
    if (message.content)
        body.append(markdownMessageContent(message));
    if (message.thread && !message.threadMessage && !message.deleted && canOpenThread(message.thread))
        body.append(threadJump(message.thread));
    if (message.attachments?.length)
        body.append(attachmentList(message.attachments));
    row.append(avatar(message.author, message.kind), body);
    return row;
}
function attachmentList(attachments) {
    const list = el("div", { className: "message-attachments" });
    for (const attachment of attachments)
        list.append(attachmentChip(attachment));
    return list;
}
function attachmentChip(attachment) {
    const downloaded = downloadedAttachment(attachment.attachmentRef);
    const label = `${attachment.filename} · ${formatBytes(attachment.size)}`;
    if (downloaded?.done) {
        const link = el("a", {
            className: "attachment-chip downloadable",
            text: `${label} · ${t(state.locale, "download")}`,
            attrs: { href: downloadURL(downloaded.chunks, downloaded.metadata.mime ?? attachment.mime), download: downloaded.metadata.filename || attachment.filename },
        });
        link.dataset.attachmentRef = attachment.attachmentRef;
        return link;
    }
    const pending = state.pendingAttachmentDownloads.has(attachment.attachmentRef);
    const button = el("button", {
        className: pending ? "attachment-chip fetching" : "attachment-chip",
        text: `${label} · ${pending ? t(state.locale, "uploadStreaming") : t(state.locale, "download")}`,
        attrs: { type: "button" },
    });
    button.dataset.attachmentRef = attachment.attachmentRef;
    button.disabled = pending || !canWrite("fetchAttachment");
    button.addEventListener("click", () => fetchMessageAttachment(attachment));
    return button;
}
function downloadedAttachment(attachmentRef) {
    for (const item of state.uploads.downloads.values()) {
        if (item.metadata.attachmentRef === attachmentRef)
            return item;
    }
    return undefined;
}
function fetchMessageAttachment(attachment) {
    if (!canWrite("fetchAttachment") || state.pendingAttachmentDownloads.has(attachment.attachmentRef))
        return;
    const downloaded = downloadedAttachment(attachment.attachmentRef);
    if (downloaded?.done) {
        triggerAttachmentDownload(attachment.attachmentRef);
        return;
    }
    state.pendingAttachmentDownloads.add(attachment.attachmentRef);
    void dispatch({ type: "fetch_discord_attachment", attachmentRef: attachment.attachmentRef });
    render();
}
function handleFetchedAttachment(metadata, done) {
    const attachmentRef = metadata.attachmentRef;
    if (!done || !attachmentRef)
        return;
    const shouldDownload = state.pendingAttachmentDownloads.delete(attachmentRef);
    if (!conversationHasAttachment(attachmentRef)) {
        pushMessage("attachment", t(state.locale, "systemAuthor"), `${metadata.filename} · ${t(state.locale, "uploadComplete")}`, {
            attachments: [{ attachmentRef, filename: metadata.filename, size: metadata.size, ...(metadata.mime ? { mime: metadata.mime } : {}) }],
        });
    }
    if (shouldDownload)
        requestAnimationFrame(() => triggerAttachmentDownload(attachmentRef));
}
function conversationHasAttachment(attachmentRef) {
    return state.messages.some((message) => message.attachments?.some((attachment) => attachment.attachmentRef === attachmentRef));
}
function triggerAttachmentDownload(attachmentRef) {
    const item = downloadedAttachment(attachmentRef);
    if (!item?.done)
        return;
    const href = downloadURL(item.chunks, item.metadata.mime);
    const link = document.createElement("a");
    link.href = href;
    link.download = item.metadata.filename;
    link.style.display = "none";
    document.body.append(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(href), 30_000);
}
function systemBlock(text, error = false) {
    return el("article", { className: error ? "system-block error" : "system-block", text });
}
function avatar(seed, kind) {
    const initials = seed.trim().slice(0, 2).toUpperCase() || "WS";
    return el("span", { className: `avatar ${kind}`, text: initials });
}
function sendDraft() {
    const text = state.draft.text.trim();
    const mode = currentDraftMode(text);
    if (!text || !modeWritable(mode))
        return;
    const targetThreadID = state.draft.targetThreadID || undefined;
    const attachments = mode === "command" ? [] : selectedAttachmentRefs(state.uploads);
    if (mode === "command") {
        const command = text.startsWith("/") ? text.slice(1).trim() : text;
        if (!command)
            return;
        void dispatch({ type: "run_bot_command", command, args: {}, ...(targetThreadID ? { targetThreadID } : {}) });
    }
    else if (mode === "agent") {
        void dispatch({ type: "send_agent_prompt", text, attachments, allowedMentions: allowedMentionSelection(text), ...(targetThreadID ? { targetThreadID } : {}) });
    }
    else {
        void dispatch({ type: "post_channel_message", text, attachments, allowedMentions: allowedMentionSelection(text), ...(targetThreadID ? { targetThreadID } : {}) });
    }
    if (mode !== "command")
        state.uploads.pending = [];
    state.draft.text = "";
    render();
}
function writableCapabilities() {
    if (!state.join?.canWrite)
        return undefined;
    return state.capabilities;
}
function canWrite(capability) {
    return Boolean(state.join?.canWrite) && hasCapability(writableCapabilities(), capability);
}
function modeCapability(mode) {
    if (mode === "command")
        return "runBotCommand";
    if (mode === "message")
        return "postChannelMessage";
    return "sendAgentPrompt";
}
function modeWritable(mode) {
    return Boolean(state.join?.canWrite) && canWrite(modeCapability(mode));
}
function composerWritable() {
    return modeWritable("message") || modeWritable("agent") || modeWritable("command");
}
function canMention(isBot) {
    return canWrite(isBot ? "mentionBot" : "mentionUsers");
}
function allowedMentionSelection(text = state.draft.text) {
    const selected = mentionSelection(state.mentionPicker);
    const bot = canWrite("mentionBot") && (Boolean(selected.bot) || draftMentionsBot(text));
    return {
        users: canWrite("mentionUsers") ? selected.users : [],
        ...(bot ? { bot: true } : {}),
    };
}
function currentDraftMode(text = state.draft.text) {
    const trimmed = text.trimStart();
    if (trimmed.startsWith("/"))
        return "command";
    if (draftMentionsBot(trimmed))
        return "agent";
    return "message";
}
function draftMentionsBot(text) {
    const bot = state.mentionPicker.bot;
    if (!bot)
        return false;
    const normalized = text.toLowerCase();
    if (normalized.includes(`<@${bot.id}>`) || normalized.includes(`<@!${bot.id}>`))
        return true;
    return botMentionAliases().some((alias) => alias !== "" && normalized.includes(`@${alias}`));
}
function botMentionAliases() {
    const bot = state.mentionPicker.bot;
    if (!bot)
        return [];
    return [bot.displayName].map((item) => item.trim().toLowerCase()).filter(Boolean);
}
function actionWithWriteToken(action) {
    if (!state.join?.writeToken)
        return action;
    const writeToken = writeTokenProof(state.join.writeToken);
    return writeToken ? { ...action, writeToken } : action;
}
function discardUpload(uploadID) {
    state.uploads.inProgress.delete(uploadID);
    const queuedIndex = state.uploads.queued.findIndex((upload) => upload.uploadID === uploadID);
    if (queuedIndex >= 0)
        state.uploads.queued.splice(queuedIndex, 1);
}
function rememberUploadedAttachment(metadata) {
    state.uploads.inProgress.delete(metadata.attachmentRef ?? "");
    if (!metadata.attachmentRef)
        return;
    const ref = { attachmentRef: metadata.attachmentRef, ref: metadata.attachmentRef, filename: metadata.filename, size: metadata.size };
    state.uploads.pending.push(metadata.mime ? { ...ref, mime: metadata.mime } : ref);
}
function statusPill() {
    const label = `${t(state.locale, "connectionStatus")}: ${t(state.locale, state.status)}`;
    return el("div", { className: "status-pill", attrs: { title: state.statusDetail ?? "" }, children: [el("span", { className: `status-dot ${state.status}` }), el("span", { text: label })] });
}
function shouldDisplayDiscordMessage(messageID) {
    if (!messageID)
        return true;
    if (state.seenMessageIDs.has(messageID))
        return false;
    state.seenMessageIDs.add(messageID);
    if (state.seenMessageIDs.size > 600)
        state.seenMessageIDs = new Set([...state.seenMessageIDs].slice(-300));
    return true;
}
function visibleMessages() {
    const selectedThreadID = state.draft.targetThreadID;
    if (!selectedThreadID)
        return state.messages.filter((message) => !message.threadMessage);
    return state.messages.filter((message) => message.threadMessage && message.thread?.id === selectedThreadID);
}
function selectedThread() {
    return state.threads.find((thread) => thread.id === state.draft.targetThreadID);
}
function canOpenThread(thread) {
    return state.threads.some((item) => item.id === thread.id);
}
function threadContextPanel(thread) {
    const { before, after } = surroundingThreadContext(thread.id);
    const panel = el("article", { className: "thread-context" });
    const back = el("button", { className: "back-button", text: t(state.locale, "backToChannel") });
    back.type = "button";
    back.addEventListener("click", () => selectThread(undefined));
    panel.append(el("div", { className: "thread-context-title", text: `${t(state.locale, "threadConversation")} # ${thread.name}` }), el("div", { className: "thread-context-subtitle", text: `${t(state.locale, "replyingInThread")} # ${thread.name}` }), back, contextSection(t(state.locale, "beforeContext"), before), contextSection(t(state.locale, "afterContext"), after));
    return panel;
}
function surroundingThreadContext(threadID) {
    const anchor = state.messages.findIndex((message) => message.thread?.id === threadID && !message.threadMessage);
    if (anchor < 0)
        return { before: [], after: [] };
    const isParentContext = (message) => !message.threadMessage && message.kind !== "system" && message.kind !== "error" && message.kind !== "upload" && message.kind !== "attachment" && message.thread?.id !== threadID;
    const before = state.messages.slice(0, anchor).filter(isParentContext).slice(-3);
    const after = state.messages.slice(anchor + 1).filter(isParentContext).slice(0, 3);
    return { before, after };
}
function contextSection(label, messages) {
    const section = el("div", { className: "context-section" });
    section.append(el("div", { className: "context-label", text: label }));
    if (messages.length === 0) {
        section.append(el("div", { className: "context-empty", text: t(state.locale, "contextNotCaptured") }));
        return section;
    }
    for (const message of messages)
        section.append(el("div", { className: "context-item", text: `${message.author}: ${displayMessageContent(message).slice(0, 160)}` }));
    return section;
}
function selectThread(threadID) {
    state.draft.targetThreadID = threadID;
    persistRoomHistory();
    if (threadID && canWrite("selectThread"))
        void dispatch({ type: "select_thread", threadID });
    render();
}
function applyDiscordMessage(kind, event, content, fallbackAuthor) {
    const action = event.action || "created";
    const existingIndex = event.messageID ? state.messages.findIndex((message) => message.discordMessageID === event.messageID) : -1;
    if (action === "deleted") {
        const deletedContent = content || t(state.locale, "messageDeleted");
        if (existingIndex >= 0) {
            const existing = state.messages[existingIndex];
            if (!existing)
                return;
            const next = { ...existing, content: deletedContent, timestamp: event.timestamp ? formatMessageTimestamp(event.timestamp) : existing.timestamp, deleted: true, thread: event.thread ?? existing.thread, threadMessage: existing.threadMessage || Boolean(event.thread && event.messageID), replyTo: event.replyTo ?? existing.replyTo };
            delete next.attachments;
            state.messages[existingIndex] = next;
            return;
        }
        pushMessage(kind, fallbackAuthor, deletedContent, { discordMessageID: event.messageID, deleted: true, mentions: event.mentions, thread: event.thread, threadMessage: Boolean(event.thread && event.messageID), replyTo: event.replyTo, timestamp: event.timestamp });
        return;
    }
    if (existingIndex >= 0) {
        const existing = state.messages[existingIndex];
        if (!existing)
            return;
        const next = {
            ...existing,
            author: event.author?.displayName ?? existing.author,
            content,
            edited: existing.edited || action === "updated",
            timestamp: event.timestamp ? formatMessageTimestamp(event.timestamp) : existing.timestamp,
            thread: event.thread ?? existing.thread,
            threadMessage: existing.threadMessage || Boolean(event.thread && event.messageID),
            replyTo: event.replyTo ?? existing.replyTo,
        };
        if (event.mentions !== undefined)
            next.mentions = event.mentions;
        if (event.attachments !== undefined) {
            if (event.attachments.length)
                next.attachments = event.attachments;
            else
                delete next.attachments;
        }
        state.messages[existingIndex] = next;
        return;
    }
    if (!shouldDisplayDiscordMessage(event.messageID))
        return;
    pushMessage(kind, event.author?.displayName ?? fallbackAuthor, content, { attachments: event.attachments, mentions: event.mentions, discordMessageID: event.messageID, edited: action === "updated", thread: event.thread, threadMessage: Boolean(event.thread && event.messageID), replyTo: event.replyTo, timestamp: event.timestamp });
}
function applyThreadEvent(event) {
    const hasMessage = Boolean(event.messageID);
    if (event.action === "deleted" && !hasMessage)
        removeThread(event.thread.id);
    else
        upsertThread(event.thread);
    if (!hasMessage) {
        if (event.action !== "selected")
            pushMessage("system", t(state.locale, "systemAuthor"), threadEventBody(event), { timestamp: event.timestamp });
        return;
    }
    applyDiscordMessage("discord", event, threadEventBody(event), event.author?.displayName ?? event.thread.name);
}
function pushMessage(kind, author, content, options = {}) {
    const message = { id: crypto.randomUUID(), kind, author, timestamp: formatMessageTimestamp(options.timestamp), content };
    if (options.attachments?.length)
        message.attachments = options.attachments;
    if (options.mentions?.length)
        message.mentions = options.mentions;
    if (options.discordMessageID)
        message.discordMessageID = options.discordMessageID;
    if (options.edited)
        message.edited = true;
    if (options.deleted)
        message.deleted = true;
    if (options.thread)
        message.thread = options.thread;
    if (options.threadMessage)
        message.threadMessage = true;
    if (options.replyTo)
        message.replyTo = options.replyTo;
    state.messages.push(message);
    if (state.messages.length > 300)
        state.messages = state.messages.slice(-300);
    persistRoomHistory();
}
function targetLabel(target) {
    const channel = target.channelName ? `#${target.channelName}` : target.channelID;
    return target.threadName ? `${channel} / ${target.threadName}` : channel;
}
function targetChannelLabel() {
    if (!state.target)
        return t(state.locale, "targetChannel");
    return state.target.channelName ?? state.target.channelID;
}
function activeTargetLabel() {
    const selected = state.threads.find((thread) => thread.id === state.draft.targetThreadID);
    return selected?.name ?? targetChannelLabel();
}
function composerPlaceholder() {
    return t(state.locale, "placeholderAgent");
}
function composerHint(mode) {
    if (mode === "command")
        return t(state.locale, "modeCommand");
    if (mode === "agent")
        return t(state.locale, "modeAgent");
    return t(state.locale, "modeMessage");
}
function threadEventBody(event) {
    if (event.content)
        return event.content;
    if (event.action === "created")
        return t(state.locale, "threadCreated");
    if (event.action === "selected")
        return t(state.locale, "threadSelected");
    if (event.action === "updated")
        return t(state.locale, "threadUpdated");
    if (event.action === "deleted")
        return t(state.locale, event.messageID ? "messageDeleted" : "threadDeleted");
    return event.action;
}
function mergeMentionables(users = state.mentionPicker.users, bot = state.mentionPicker.bot) {
    const byID = new Map(state.mentionPicker.users.map((user) => [user.id, user]));
    for (const user of users)
        byID.set(user.id, user);
    state.mentionPicker.users = [...byID.values()].slice(0, 100);
    if (bot)
        state.mentionPicker.bot = bot;
}
function upsertThread(thread) {
    const index = state.threads.findIndex((item) => item.id === thread.id);
    const existing = index >= 0 ? state.threads[index] : undefined;
    const name = thread.name && thread.name !== thread.id ? thread.name : existing?.name ?? thread.name ?? thread.id;
    const next = { ...thread, name };
    if (index >= 0)
        state.threads[index] = next;
    else
        state.threads.unshift(next);
}
function removeThread(threadID) {
    state.threads = state.threads.filter((thread) => thread.id !== threadID);
    if (state.draft.targetThreadID === threadID)
        state.draft.targetThreadID = undefined;
}
function welcomeSelectedThread(event, previousSelectedThreadID) {
    if (event.target.threadID)
        return event.target.threadID;
    if (event.selectedThreadID)
        return event.selectedThreadID;
    if (previousSelectedThreadID && event.threads?.some((thread) => thread.id === previousSelectedThreadID))
        return previousSelectedThreadID;
    return undefined;
}
function formatMessageTimestamp(raw) {
    if (!raw)
        return new Date().toLocaleString();
    const date = new Date(raw);
    if (Number.isNaN(date.getTime()))
        return new Date().toLocaleString();
    return date.toLocaleString();
}
function roomHistoryKey(roomID = state.join?.roomID) {
    if (!roomID)
        return undefined;
    return `${roomHistoryPrefix}${roomID}:v${roomHistoryVersion}`;
}
function restoreRoomHistory() {
    const key = roomHistoryKey();
    if (!key)
        return;
    try {
        const raw = sessionStorage.getItem(key);
        if (!raw)
            return;
        const parsed = JSON.parse(raw);
        if (parsed.v !== roomHistoryVersion)
            return;
        state.messages = sanitizeStoredMessages(parsed.messages).slice(-roomHistoryLimit);
        state.threads = sanitizeStoredThreads(parsed.threads);
        state.draft.targetThreadID = typeof parsed.selectedThreadID === "string" ? parsed.selectedThreadID : undefined;
        state.seenMessageIDs = new Set(state.messages.map((message) => message.discordMessageID).filter((id) => Boolean(id)));
    }
    catch {
        // Ignore corrupt per-room browser history; live relay state remains authoritative.
    }
}
function persistRoomHistory() {
    const key = roomHistoryKey();
    if (!key)
        return;
    try {
        const payload = {
            v: roomHistoryVersion,
            messages: state.messages.slice(-roomHistoryLimit),
            threads: state.threads,
            ...(state.draft.targetThreadID ? { selectedThreadID: state.draft.targetThreadID } : {}),
            savedAt: new Date().toISOString(),
        };
        sessionStorage.setItem(key, JSON.stringify(payload));
    }
    catch {
        // Storage can be disabled or full; never break the live control surface.
    }
}
function sanitizeStoredMessages(value) {
    if (!Array.isArray(value))
        return [];
    const messages = [];
    for (const item of value) {
        if (!item || typeof item !== "object")
            continue;
        const raw = item;
        if (!isMessageKind(raw.kind) || typeof raw.author !== "string" || typeof raw.timestamp !== "string" || typeof raw.content !== "string")
            continue;
        messages.push({
            id: typeof raw.id === "string" ? raw.id : crypto.randomUUID(),
            kind: raw.kind,
            author: raw.author,
            timestamp: raw.timestamp,
            content: raw.content,
            ...(Array.isArray(raw.attachments) ? { attachments: raw.attachments } : {}),
            ...(Array.isArray(raw.mentions) ? { mentions: raw.mentions } : {}),
            ...(typeof raw.discordMessageID === "string" ? { discordMessageID: raw.discordMessageID } : {}),
            ...(raw.edited ? { edited: true } : {}),
            ...(raw.deleted ? { deleted: true } : {}),
            ...(isThreadView(raw.thread) ? { thread: raw.thread } : {}),
            ...(raw.threadMessage ? { threadMessage: true } : {}),
            ...(raw.replyTo ? { replyTo: raw.replyTo } : {}),
        });
    }
    return messages;
}
function sanitizeStoredThreads(value) {
    if (!Array.isArray(value))
        return [];
    return value.filter(isThreadView);
}
function isThreadView(value) {
    if (!value || typeof value !== "object")
        return false;
    const thread = value;
    return typeof thread.id === "string" && typeof thread.name === "string";
}
function isMessageKind(value) {
    return value === "discord" || value === "agent" || value === "command" || value === "system" || value === "upload" || value === "attachment" || value === "error";
}
function markdownMessageContent(message) {
    const root = el("div", { className: "message-content markdown-content" });
    root.append(...renderMarkdownBlocks(displayMessageContent(message)));
    return root;
}
function renderMarkdownBlocks(markdown) {
    const lines = markdown.replace(/\r\n?/g, "\n").split("\n");
    const nodes = [];
    for (let index = 0; index < lines.length;) {
        const line = lines[index] ?? "";
        if (line.trim() === "") {
            index += 1;
            continue;
        }
        const fence = line.match(/^\s*```(.*)$/);
        if (fence) {
            const code = [];
            const language = (fence[1] ?? "").trim().match(/^[A-Za-z0-9_-]+$/)?.[0];
            index += 1;
            while (index < lines.length && !/^\s*```\s*$/.test(lines[index] ?? "")) {
                code.push(lines[index] ?? "");
                index += 1;
            }
            if (index < lines.length)
                index += 1;
            const pre = el("pre");
            const codeNode = el("code", { text: code.join("\n") });
            if (language)
                codeNode.dataset.language = language;
            pre.append(codeNode);
            nodes.push(pre);
            continue;
        }
        const heading = line.match(/^(#{1,6})\s+(.+)$/);
        if (heading) {
            const level = Math.min(6, heading[1]?.length ?? 1);
            const node = document.createElement(`h${level}`);
            node.append(...renderInlineMarkdown(heading[2] ?? ""));
            nodes.push(node);
            index += 1;
            continue;
        }
        if (/^\s*>\s?/.test(line)) {
            const quoteLines = [];
            while (index < lines.length && /^\s*>\s?/.test(lines[index] ?? "")) {
                quoteLines.push((lines[index] ?? "").replace(/^\s*>\s?/, ""));
                index += 1;
            }
            const blockquote = el("blockquote");
            blockquote.append(...renderMarkdownBlocks(quoteLines.join("\n")));
            nodes.push(blockquote);
            continue;
        }
        const unordered = line.match(/^\s*[-*+]\s+(.+)$/);
        const ordered = line.match(/^\s*\d+[.)]\s+(.+)$/);
        if (unordered || ordered) {
            const list = document.createElement(ordered ? "ol" : "ul");
            const pattern = ordered ? /^\s*\d+[.)]\s+(.+)$/ : /^\s*[-*+]\s+(.+)$/;
            while (index < lines.length) {
                const item = (lines[index] ?? "").match(pattern);
                if (!item)
                    break;
                const li = el("li");
                li.append(...renderInlineMarkdown(item[1] ?? ""));
                list.append(li);
                index += 1;
            }
            nodes.push(list);
            continue;
        }
        const paragraph = [];
        while (index < lines.length && lines[index]?.trim() !== "" && !/^\s*```/.test(lines[index] ?? "") && !/^(#{1,6})\s+.+/.test(lines[index] ?? "") && !/^\s*>\s?/.test(lines[index] ?? "") && !/^\s*(?:[-*+]\s+.+|\d+[.)]\s+.+)/.test(lines[index] ?? "")) {
            paragraph.push(lines[index] ?? "");
            index += 1;
        }
        const p = el("p");
        paragraph.forEach((text, lineIndex) => {
            if (lineIndex > 0)
                p.append(document.createElement("br"));
            p.append(...renderInlineMarkdown(text));
        });
        nodes.push(p);
    }
    return nodes.length ? nodes : [document.createTextNode("")];
}
function renderInlineMarkdown(text, depth = 0) {
    if (!text)
        return [];
    if (depth > 8)
        return [document.createTextNode(text)];
    const nodes = [];
    let rest = text;
    while (rest) {
        const token = nextMarkdownToken(rest);
        if (!token) {
            nodes.push(document.createTextNode(rest));
            break;
        }
        if (token.index > 0)
            nodes.push(document.createTextNode(rest.slice(0, token.index)));
        const raw = rest.slice(token.index, token.end);
        if (token.kind === "code") {
            nodes.push(el("code", { text: token.content }));
        }
        else if (token.kind === "link") {
            const safeHref = safeMarkdownHref(token.href);
            if (safeHref) {
                const link = el("a", { attrs: { href: safeHref, target: "_blank", rel: "noreferrer noopener" } });
                link.append(...renderInlineMarkdown(token.content, depth + 1));
                nodes.push(link);
            }
            else {
                nodes.push(document.createTextNode(token.content));
            }
        }
        else {
            const node = document.createElement(token.kind === "strong" ? "strong" : "em");
            node.append(...renderInlineMarkdown(token.content, depth + 1));
            nodes.push(node);
        }
        rest = rest.slice(token.end || raw.length);
    }
    return nodes;
}
function nextMarkdownToken(text) {
    const candidates = [
        codeSpanToken(text),
        linkToken(text),
        delimitedToken(text, "**", "strong"),
        delimitedToken(text, "__", "strong"),
        delimitedToken(text, "*", "em"),
        delimitedToken(text, "_", "em"),
    ].filter((token) => Boolean(token));
    candidates.sort((a, b) => a.index - b.index || b.end - a.end);
    return candidates[0];
}
function codeSpanToken(text) {
    const start = text.indexOf("`");
    if (start < 0)
        return undefined;
    const end = text.indexOf("`", start + 1);
    if (end <= start)
        return undefined;
    return { kind: "code", index: start, end: end + 1, content: text.slice(start + 1, end) };
}
function linkToken(text) {
    const match = /\[([^\]\n]+)\]\(([^()\s]+)\)/.exec(text);
    if (!match || match.index === undefined)
        return undefined;
    return { kind: "link", index: match.index, end: match.index + match[0].length, content: match[1] ?? "", href: match[2] ?? "" };
}
function delimitedToken(text, delimiter, kind) {
    const start = text.indexOf(delimiter);
    if (start < 0)
        return undefined;
    const contentStart = start + delimiter.length;
    const end = text.indexOf(delimiter, contentStart);
    if (end <= contentStart)
        return undefined;
    if (delimiter.length === 1 && text[start + 1] === delimiter)
        return undefined;
    return { kind, index: start, end: end + delimiter.length, content: text.slice(contentStart, end) };
}
function safeMarkdownHref(href) {
    try {
        const url = new URL(href, window.location.href);
        if (url.protocol === "http:" || url.protocol === "https:" || url.protocol === "mailto:")
            return url.href;
    }
    catch {
        return undefined;
    }
    return undefined;
}
function displayMessageContent(message) {
    if (!message.content)
        return "";
    const mentions = new Map();
    for (const mention of message.mentions ?? [])
        mentions.set(mention.id, mention);
    if (state.mentionPicker.bot)
        mentions.set(state.mentionPicker.bot.id, { ...state.mentionPicker.bot, bot: true, kind: "bot" });
    for (const user of state.mentionPicker.users)
        mentions.set(user.id, { ...user, kind: "user" });
    return message.content.replace(/<(@!?|@&|#)(\d+)>/g, (token, prefix, id) => {
        const mention = mentions.get(id);
        if (mention) {
            if (mention.kind === "channel" || prefix === "#")
                return `#${mention.displayName}`;
            return `@${mention.displayName}`;
        }
        if (prefix === "#")
            return "#channel";
        if (prefix === "@&")
            return "@role";
        return "@Discord user";
    });
}
function threadJump(thread) {
    const button = el("button", { className: "thread-jump", text: `${t(state.locale, "openThread")} · ${t(state.locale, "replyInThread")} # ${thread.name}` });
    button.type = "button";
    button.addEventListener("click", () => selectThread(thread.id));
    return button;
}
function replyPreview(replyTo) {
    const author = replyTo.author?.displayName ?? t(state.locale, "repliedMessage");
    const content = replyTo.deleted ? t(state.locale, "messageDeleted") : replyTo.content ?? replyTo.messageID;
    return el("div", { className: "reply-preview", text: `${t(state.locale, "replyingTo")} ${author}: ${content.slice(0, 160)}` });
}
