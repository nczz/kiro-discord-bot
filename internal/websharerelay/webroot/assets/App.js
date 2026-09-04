import { continueAcceptedUpload, downloadURL, queueUploads, selectedAttachmentRefs } from "./attachments.js";
import { deriveSessionKeys, openJSON, parseJoinFragment, sealJSON, writeTokenProof } from "./crypto.js";
import { clear, el, formatBytes } from "./dom.js";
import { allowedMentionSelectionForDraft, commandName, displayDiscordMentions, draftMentionsBot, highRiskCommand, mentionPreviewNames, resolveDraftMode, webshareCommandAllowed } from "./composer.js";
import { parseDiscordMessageReference, suggestedThreadName } from "./threads.js";
import { chooseLocale, setLocale, t } from "./i18n.js";
import { hasCapability, PROTOCOL_VERSION } from "./protocol.js";
import { WebShareTransport } from "./transport.js";
const roomHistoryVersion = 1;
const roomHistoryLimit = 300;
const roomHistoryPrefix = "kdb-webshare:room:";
const commandSuggestions = [
    "cron-list",
    "cron-run",
    "remind",
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
    share: undefined,
    target: undefined,
    opener: undefined,
    threads: [],
    mentionPicker: { users: [], bot: undefined, selectedUsers: new Set(), botSelected: false },
    draft: { mode: "message", text: "", targetThreadID: undefined, newThreadName: "", sourceMessageID: "", threadError: undefined },
    uploads: { pending: [], queued: [], inProgress: new Map(), downloads: new Map() },
    pendingAttachmentDownloads: new Set(),
    messages: [],
    error: undefined,
    seenMessageIDs: new Set(),
    clientPeerID: randomClientPeerID(),
    terminalReason: undefined,
    liveAnnouncement: undefined,
    mobileThreadsOpen: false,
    unseenCount: 0,
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
                setError(error instanceof Error ? error.message : String(error));
                render();
            },
        });
        state.transport.connect();
    }
    catch (error) {
        setError(error instanceof Error ? error.message : String(error));
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
        setError(t(state.locale, "viewOnly"));
        render();
        return;
    }
    try {
        const signedAction = actionWithWriteToken(action);
        const seq = state.sendSeq;
        state.sendSeq += 1;
        const payload = await sealJSON(signedAction, state.keys.guestToHost, state.join.roomID, state.clientPeerID, seq, 2, "guest_to_host");
        if (!state.transport.send(payload, state.hostPeerID ?? 0))
            setError(t(state.locale, "notConnected"));
    }
    catch (error) {
        setError(error instanceof Error ? error.message : String(error));
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
        setError(error instanceof Error ? error.message : String(error));
    }
    render();
}
function applyServerEvent(event) {
    switch (event.type) {
        case "welcome": {
            const previousSelectedThreadID = state.draft.targetThreadID;
            state.share = event.share;
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
            pushMessage("agent", t(state.locale, "agentAuthor"), event.event.content ?? event.event.status, { timestamp: event.event.timestamp, mentions: event.event.mentions });
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
        case "error": {
            state.pendingAttachmentDownloads.clear();
            const message = `${event.reasonCode ?? event.code ?? "error"}: ${event.content ?? event.messageKey ?? ""}`;
            setError(message);
            pushMessage("error", t(state.locale, "systemAuthor"), message);
            break;
        }
        case "bye": {
            state.terminalReason = event.reasonCode;
            const message = terminalMessage(event.reasonCode);
            setError(message);
            pushMessage("system", t(state.locale, "systemAuthor"), message);
            state.transport?.stop();
            break;
        }
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
    const announcement = state.liveAnnouncement;
    const shouldStickToBottom = !oldList || isScrolledNearBottom(oldList);
    const previousScrollTop = oldList?.scrollTop ?? 0;
    clear(app);
    const shell = discordShell();
    app.append(shell);
    const list = shell.querySelector(".message-list");
    if (!list)
        return;
    if (shouldStickToBottom) {
        state.unseenCount = 0;
        scrollMessageListToBottom(list);
    }
    else {
        list.scrollTop = previousScrollTop;
    }
    if (announcement) {
        window.setTimeout(() => {
            if (state.liveAnnouncement !== announcement)
                return;
            state.liveAnnouncement = undefined;
            render();
        }, 4000);
    }
}
function setError(message) {
    state.error = message;
    state.liveAnnouncement = message;
}
function discordShell() {
    const shell = el("main", { className: "discord-app" });
    shell.append(serverRail(), channelSidebar(), chatPane(), memberSidebar());
    return shell;
}
function serverRail() {
    const rail = el("aside", { className: "server-rail", attrs: { "aria-label": serverLabel() } });
    rail.append(el("div", { className: "server-icon active", text: serverInitials(), attrs: { title: serverLabel(), "aria-label": serverLabel() } }));
    return rail;
}
function channelSidebar() {
    const nav = el("nav", { className: "channel-sidebar", attrs: { "aria-label": t(state.locale, "channels") } });
    nav.append(el("div", { className: "guild-title", text: serverLabel() }));
    nav.append(channelButton(undefined, targetChannelLabel(), !state.draft.targetThreadID));
    nav.append(el("div", { className: "sidebar-section", text: t(state.locale, "threads") }));
    for (const thread of state.threads)
        nav.append(channelButton(thread.id, thread.name, state.draft.targetThreadID === thread.id));
    nav.append(createThreadInline());
    return nav;
}
function channelButton(threadID, label, active) {
    const button = el("button", { className: active ? "channel-link active" : "channel-link", text: `# ${label}` });
    button.type = "button";
    button.disabled = Boolean(threadID) && !canWrite("selectThread");
    button.addEventListener("click", () => selectThread(threadID));
    return button;
}
function createThreadInline() {
    const box = el("div", { className: "thread-create" });
    const name = el("input", { attrs: { placeholder: t(state.locale, "threadName"), "aria-label": t(state.locale, "threadName") } });
    const source = el("input", { attrs: { placeholder: t(state.locale, "sourceMessageCompact"), "aria-label": t(state.locale, "sourceMessage") } });
    const create = el("button", { text: t(state.locale, "createThread"), attrs: { "aria-label": t(state.locale, "createThread") } });
    name.value = state.draft.newThreadName;
    source.value = state.draft.sourceMessageID;
    name.addEventListener("input", () => {
        state.draft.newThreadName = name.value;
        state.draft.threadError = undefined;
    });
    source.addEventListener("input", () => {
        state.draft.sourceMessageID = source.value;
        state.draft.threadError = undefined;
    });
    create.disabled = !canWrite("createThread");
    create.addEventListener("click", () => {
        const threadName = state.draft.newThreadName.trim();
        if (!threadName)
            return;
        const sourceInput = state.draft.sourceMessageID.trim();
        const sourceMessageID = parseDiscordMessageReference(sourceInput);
        if (sourceInput && !sourceMessageID) {
            state.draft.threadError = t(state.locale, "sourceMessageInvalid");
            render();
            return;
        }
        void dispatch({ type: "create_thread", name: threadName, ...(sourceMessageID ? { sourceMessageID } : {}) });
        state.draft.newThreadName = "";
        state.draft.sourceMessageID = "";
        state.draft.threadError = undefined;
        render();
    });
    box.append(el("div", { className: "hint", text: t(state.locale, "threadCreateHint") }), name, source);
    if (state.draft.threadError)
        box.append(el("div", { className: "thread-error", text: state.draft.threadError, attrs: { role: "alert" } }));
    box.append(create);
    return box;
}
function chatPane() {
    const pane = el("section", { className: "chat-pane" });
    pane.append(chatHeader());
    const list = el("div", { className: "message-list", attrs: { role: "log", "aria-live": "off", "aria-relevant": "additions text", "aria-label": `${t(state.locale, "eventLog")} · ${activeTargetLabel()}` } });
    const thread = selectedThread();
    list.append(delegationNotice());
    if (state.mobileThreadsOpen)
        list.append(mobileThreadPanel());
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
    if (state.liveAnnouncement)
        pane.append(liveAnnouncementRegion());
    if (state.unseenCount > 0)
        pane.append(newMessagesButton());
    pane.append(list, composerBar());
    return pane;
}
function isScrolledNearBottom(list) {
    return list.scrollHeight - list.scrollTop - list.clientHeight < 80;
}
function scrollMessageListToBottom(list) {
    list.scrollTop = list.scrollHeight;
    requestAnimationFrame(() => {
        list.scrollTop = list.scrollHeight;
    });
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
    const locale = el("select", { className: "locale-select", attrs: { "aria-label": t(state.locale, "locale") } });
    locale.append(new Option("English", "en"), new Option("繁體中文", "zh-TW"));
    locale.value = state.locale;
    locale.addEventListener("change", () => {
        state.locale = locale.value === "zh-TW" ? "zh-TW" : "en";
        setLocale(state.locale);
        render();
    });
    const actions = [threadMenuButton(), statusPill(), locale, ...safetyControls()];
    if (thread) {
        const back = el("button", { className: "back-button", text: t(state.locale, "backToChannel") });
        back.type = "button";
        back.addEventListener("click", () => selectThread(undefined));
        actions.unshift(back);
    }
    header.append(title, el("div", { className: "header-actions", children: actions }));
    return header;
}
function threadMenuButton() {
    const button = el("button", { className: "thread-menu-button", text: t(state.locale, "mobileThreads"), attrs: { "aria-expanded": String(state.mobileThreadsOpen), "aria-label": t(state.locale, "mobileThreads") } });
    button.type = "button";
    button.addEventListener("click", () => {
        const opening = !state.mobileThreadsOpen;
        state.mobileThreadsOpen = opening;
        render();
        requestAnimationFrame(() => app.querySelector(opening ? ".mobile-thread-close" : ".thread-menu-button")?.focus());
    });
    return button;
}
function safetyControls() {
    if (!state.join?.canWrite)
        return [];
    const interrupt = el("button", { className: "safety-button", text: t(state.locale, "interruptAgent"), attrs: { "aria-label": t(state.locale, "interruptAgent") } });
    interrupt.type = "button";
    interrupt.disabled = state.terminalReason !== undefined || !canWrite("interruptAgent");
    interrupt.addEventListener("click", () => {
        void dispatch({ type: "interrupt_agent" });
    });
    const stop = el("button", { className: "safety-button", text: t(state.locale, "stopShare"), attrs: { "aria-label": t(state.locale, "stopShare") } });
    stop.type = "button";
    stop.disabled = state.terminalReason !== undefined;
    stop.addEventListener("click", () => {
        if (!window.confirm(`${t(state.locale, "stopConfirm")}\n${t(state.locale, "sendTarget")}: ${draftTargetLabel()}`))
            return;
        void dispatch({ type: "stop" });
    });
    const revoke = el("button", { className: "safety-button danger", text: t(state.locale, "revokeShare"), attrs: { "aria-label": t(state.locale, "revokeShare") } });
    revoke.type = "button";
    revoke.disabled = state.terminalReason !== undefined;
    revoke.addEventListener("click", () => {
        if (!window.confirm(t(state.locale, "revokeConfirm")))
            return;
        void dispatch({ type: "revoke" });
    });
    return [interrupt, stop, revoke];
}
function mobileThreadPanel() {
    const panel = el("section", { className: "mobile-thread-panel", attrs: { "aria-label": t(state.locale, "mobileThreads") } });
    panel.append(el("div", {
        className: "mobile-thread-panel-header",
        children: [
            el("strong", { text: t(state.locale, "mobileThreads") }),
            el("button", { className: "mobile-thread-close", text: "×", attrs: { type: "button", "aria-label": t(state.locale, "closeThreadDrawer") } }),
        ],
    }));
    const close = panel.querySelector(".mobile-thread-close");
    close?.addEventListener("click", () => {
        state.mobileThreadsOpen = false;
        render();
        requestAnimationFrame(() => app.querySelector(".thread-menu-button")?.focus());
    });
    panel.append(channelButton(undefined, targetChannelLabel(), !state.draft.targetThreadID));
    for (const thread of state.threads)
        panel.append(channelButton(thread.id, thread.name, state.draft.targetThreadID === thread.id));
    panel.append(createThreadInline());
    return panel;
}
function newMessagesButton() {
    const label = `${t(state.locale, "newMessages")} · ${t(state.locale, "jumpToLatest")}`;
    const button = el("button", { className: "new-messages-button", text: label, attrs: { "aria-label": label } });
    button.type = "button";
    button.addEventListener("click", () => {
        state.unseenCount = 0;
        const list = app.querySelector(".message-list");
        if (list)
            scrollMessageListToBottom(list);
        render();
    });
    return button;
}
function liveAnnouncementRegion() {
    return el("div", { className: "sr-only", text: state.liveAnnouncement ?? "", attrs: { role: "status", "aria-live": "polite", "aria-atomic": "true" } });
}
function routeSelector(disabled) {
    const group = el("div", { className: "route-selector", attrs: { role: "radiogroup", "aria-label": t(state.locale, "routeLabel") } });
    const routes = [["message", t(state.locale, "channelMessage")], ["agent", t(state.locale, "agentPrompt")], ["command", t(state.locale, "botCommand")]];
    for (const [mode, label] of routes) {
        const active = state.draft.mode === mode;
        const button = el("button", { className: active ? "route-chip active" : "route-chip", text: label, attrs: { role: "radio", "aria-checked": String(active), "data-mode": mode, tabindex: active ? "0" : "-1" } });
        button.type = "button";
        button.disabled = disabled || !modeWritable(mode);
        button.tabIndex = active && !button.disabled ? 0 : -1;
        button.addEventListener("click", () => {
            state.draft.mode = mode;
            render();
            requestAnimationFrame(() => app.querySelector(`.route-chip[data-mode="${mode}"]`)?.focus());
        });
        group.append(button);
    }
    group.addEventListener("keydown", (event) => {
        let offset = 0;
        switch (event.key) {
            case "ArrowRight":
            case "ArrowDown":
                offset = 1;
                break;
            case "ArrowLeft":
            case "ArrowUp":
                offset = -1;
                break;
            default:
                return;
        }
        event.preventDefault();
        const enabled = routes.map(([mode]) => mode).filter((mode) => modeWritable(mode));
        if (enabled.length === 0)
            return;
        const current = Math.max(0, enabled.indexOf(state.draft.mode));
        const nextMode = enabled[(current + offset + enabled.length) % enabled.length] ?? state.draft.mode;
        state.draft.mode = nextMode;
        render();
        requestAnimationFrame(() => app.querySelector(`.route-chip[data-mode="${nextMode}"]`)?.focus());
    });
    return group;
}
function syncRouteSelector(group, disabled) {
    for (const button of group.querySelectorAll(".route-chip")) {
        const mode = button.dataset.mode;
        if (!mode)
            continue;
        const active = state.draft.mode === mode;
        button.classList.toggle("active", active);
        button.setAttribute("aria-checked", String(active));
        button.disabled = disabled || !modeWritable(mode);
        button.tabIndex = active && !button.disabled ? 0 : -1;
    }
}
function delegationNotice() {
    const mode = state.join?.canWrite ? t(state.locale, "writeLink") : t(state.locale, "viewOnly");
    const status = state.terminalReason ? terminalMessage(state.terminalReason) : shareStatusLabel();
    return el("article", {
        className: "delegation-notice trust-bar",
        children: [
            el("strong", { text: `${t(state.locale, "delegationWarningTitle")} · ${mode}` }),
            el("span", { text: `${t(state.locale, "server")}: ${serverLabel()}` }),
            el("span", { text: `${t(state.locale, "target")}: ${targetLabel(state.target)} · ${t(state.locale, "targetType")}: ${targetTypeLabel()}` }),
            el("span", { text: `${t(state.locale, "shareIdentity")}: ${state.opener?.displayName ?? "-"} ${t(state.locale, "writeAs")} WebShare` }),
            el("span", { text: `${t(state.locale, "shareStatus")}: ${status}` }),
            ...(!composerWritable() ? [el("span", { className: "muted", text: t(state.locale, "writeUnavailable") })] : []),
            el("span", { text: t(state.locale, "delegationWarningBody") }),
            el("span", { className: "muted", text: t(state.locale, "partialMirror") }),
        ],
    });
}
function composerBar() {
    const inferredMode = currentDraftMode();
    const disabled = !composerWritable();
    const wrap = el("form", { className: "composer-bar" });
    wrap.addEventListener("submit", (event) => {
        event.preventDefault();
        sendDraft();
    });
    const route = routeSelector(disabled);
    const hint = el("div", { className: "composer-hint", text: composerHint(inferredMode), attrs: { id: "composer-hint" } });
    const inputRow = el("div", { className: "composer-input-row" });
    const upload = uploadControl(disabled || inferredMode === "command" || !canWrite("upload"));
    const textarea = el("textarea", { attrs: { placeholder: composerPlaceholder(), rows: "1", autocomplete: "off", spellcheck: "true", "aria-label": t(state.locale, "messageText"), "aria-describedby": "composer-hint", role: "combobox", "aria-autocomplete": "list", "aria-controls": "composer-suggestions", "aria-expanded": "false" } });
    const suggestions = el("div", { className: "composer-suggestions hidden", attrs: { role: "listbox", id: "composer-suggestions" } });
    textarea.value = state.draft.text;
    textarea.disabled = disabled;
    const utility = inlineUtilityRow();
    let activeSuggestionIndex = 0;
    const refreshComposer = () => {
        state.draft.text = textarea.value;
        const mode = currentDraftMode(textarea.value);
        hint.textContent = composerHint(mode);
        setUploadControlDisabled(upload, disabled || mode === "command" || !canWrite("upload"));
        syncRouteSelector(route, disabled);
        renderInlineUtilityRow(utility);
        activeSuggestionIndex = 0;
        renderComposerSuggestions(textarea, suggestions, refreshComposer, activeSuggestionIndex);
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
    textarea.addEventListener("click", () => {
        activeSuggestionIndex = 0;
        renderComposerSuggestions(textarea, suggestions, refreshComposer, activeSuggestionIndex);
    });
    textarea.addEventListener("keyup", (event) => {
        switch (event.key) {
            case "ArrowDown":
            case "ArrowUp":
            case "Enter":
            case "Escape":
            case "Tab":
                return;
            default:
                activeSuggestionIndex = 0;
                renderComposerSuggestions(textarea, suggestions, refreshComposer, activeSuggestionIndex);
        }
    });
    textarea.addEventListener("keydown", (event) => {
        const suggestionCount = composerSuggestions(textarea).length;
        const suggestionsOpen = suggestionCount > 0 && !suggestions.classList.contains("hidden");
        if (suggestionsOpen && event.key === "ArrowDown") {
            event.preventDefault();
            activeSuggestionIndex = (activeSuggestionIndex + 1) % suggestionCount;
            renderComposerSuggestions(textarea, suggestions, refreshComposer, activeSuggestionIndex);
            return;
        }
        if (suggestionsOpen && event.key === "ArrowUp") {
            event.preventDefault();
            activeSuggestionIndex = (activeSuggestionIndex + suggestionCount - 1) % suggestionCount;
            renderComposerSuggestions(textarea, suggestions, refreshComposer, activeSuggestionIndex);
            return;
        }
        if (event.key === "Escape" && suggestionsOpen) {
            event.preventDefault();
            hideComposerSuggestions(textarea, suggestions);
            return;
        }
        if (event.key === "Tab" && suggestionsOpen && acceptFirstComposerSuggestion(textarea)) {
            event.preventDefault();
            refreshComposer();
            return;
        }
        if (event.key === "Enter" && !event.shiftKey) {
            if (composing || event.isComposing || event.keyCode === 229)
                return;
            if (suggestionsOpen && acceptComposerSuggestionAt(textarea, activeSuggestionIndex)) {
                event.preventDefault();
                refreshComposer();
                return;
            }
            event.preventDefault();
            sendDraft();
        }
    });
    const send = el("button", { className: "send-button", text: t(state.locale, "send") });
    send.disabled = disabled;
    inputRow.append(upload, textarea, send);
    wrap.append(route, hint);
    if (inferredMode !== "command")
        wrap.append(attachmentChips());
    wrap.append(suggestions, inputRow, utility);
    renderComposerSuggestions(textarea, suggestions, refreshComposer, activeSuggestionIndex);
    return wrap;
}
function setUploadControlDisabled(control, disabled) {
    control.classList.toggle("disabled", disabled);
    control.setAttribute("aria-disabled", String(disabled));
    control.tabIndex = disabled ? -1 : 0;
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
function renderComposerSuggestions(textarea, root, refresh, activeIndex = 0) {
    clear(root);
    const suggestions = composerSuggestions(textarea);
    root.classList.toggle("hidden", suggestions.length === 0);
    textarea.setAttribute("aria-expanded", String(suggestions.length > 0));
    if (suggestions.length === 0) {
        textarea.removeAttribute("aria-activedescendant");
        return;
    }
    const selectedIndex = Math.min(activeIndex, suggestions.length - 1);
    textarea.setAttribute("aria-activedescendant", `composer-suggestion-${selectedIndex}`);
    suggestions.forEach((suggestion, index) => {
        const selected = index === selectedIndex;
        const button = el("button", { className: selected ? "composer-suggestion active" : "composer-suggestion", attrs: { id: `composer-suggestion-${index}`, type: "button", role: "option", "aria-selected": String(selected) }, children: [el("strong", { text: suggestion.label }), el("span", { text: suggestion.detail })] });
        button.addEventListener("mousedown", (event) => event.preventDefault());
        button.addEventListener("click", () => {
            acceptComposerSuggestion(textarea, suggestion);
            refresh();
            textarea.focus();
        });
        root.append(button);
    });
}
function hideComposerSuggestions(textarea, root) {
    clear(root);
    root.classList.add("hidden");
    textarea.setAttribute("aria-expanded", "false");
    textarea.removeAttribute("aria-activedescendant");
}
function acceptFirstComposerSuggestion(textarea) {
    return acceptComposerSuggestionAt(textarea, 0);
}
function acceptComposerSuggestionAt(textarea, index) {
    const suggestion = composerSuggestions(textarea)[index];
    return suggestion ? acceptComposerSuggestion(textarea, suggestion) : false;
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
    const label = el("label", { className: disabled ? "upload-icon disabled" : "upload-icon", text: "+", attrs: { role: "button", tabindex: disabled ? "-1" : "0", "aria-disabled": String(disabled), "aria-label": t(state.locale, "attachFiles"), title: t(state.locale, "attachFiles") } });
    const input = el("input", { attrs: { type: "file", multiple: "true", "aria-label": t(state.locale, "attachFiles") } });
    input.disabled = disabled;
    label.addEventListener("keydown", (event) => {
        if (input.disabled || (event.key !== "Enter" && event.key !== " "))
            return;
        event.preventDefault();
        input.click();
    });
    input.addEventListener("change", () => void queueUploads(input.files, state.uploads, (action) => dispatch(action)));
    label.append(input);
    return label;
}
function inlineUtilityRow() {
    const row = el("div", { className: "composer-utility" });
    renderInlineUtilityRow(row);
    return row;
}
function renderInlineUtilityRow(row) {
    clear(row);
    row.append(inlineMentionFallback(), el("span", { className: "target-chip", text: `${t(state.locale, "sendTarget")}: ${draftTargetLabel()}` }), el("span", { className: "target-chip", text: `${t(state.locale, "writeAs")} ${state.opener?.displayName ?? "-"}` }));
    const pings = mentionPreview();
    if (pings)
        row.append(pings);
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
    const createThread = createThreadFromMessageButton(message);
    if (createThread)
        body.append(el("div", { className: "message-actions", children: [createThread] }));
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
    if (error)
        return el("article", { className: "system-block error", text, attrs: { role: "alert" } });
    return el("article", { className: "system-block", text });
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
        const name = commandName(command);
        if (!command || !name)
            return;
        if (!webshareCommandAllowed(name)) {
            const message = t(state.locale, "commandNotAllowed");
            setError(message);
            pushMessage("error", t(state.locale, "systemAuthor"), message);
            render();
            return;
        }
        if (highRiskCommand(name) && !window.confirm(`${t(state.locale, "commandConfirm")}\n${t(state.locale, "sendTarget")}: ${draftTargetLabel()}\n/${command}`))
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
    state.mentionPicker.selectedUsers.clear();
    state.mentionPicker.botSelected = false;
    state.draft.text = "";
    render();
}
function writableCapabilities() {
    if (!state.join?.canWrite)
        return undefined;
    return state.capabilities;
}
function canWrite(capability) {
    return state.terminalReason === undefined && shareWritableState() && Boolean(state.join?.canWrite) && hasCapability(writableCapabilities(), capability);
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
    return allowedMentionSelectionForDraft(text, state.mentionPicker, canWrite("mentionUsers"), canWrite("mentionBot"));
}
function currentDraftMode(text = state.draft.text) {
    const mode = resolveDraftMode(state.draft.mode, text, modeWritable("agent") ? state.mentionPicker.bot : undefined);
    if (mode === "command" && mode !== state.draft.mode)
        state.draft.mode = mode;
    return mode;
}
function actionWithWriteToken(action) {
    const eventID = action.type === "hello" ? action.eventID : action.eventID ?? crypto.randomUUID();
    const withID = eventID ? { ...action, eventID } : action;
    if (!state.join?.writeToken)
        return withID;
    const writeToken = writeTokenProof(state.join.writeToken);
    return writeToken ? { ...withID, writeToken } : withID;
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
    const label = state.terminalReason ? `${t(state.locale, "connectionStatus")}: ${terminalMessage(state.terminalReason)}` : `${t(state.locale, "connectionStatus")}: ${t(state.locale, state.status)}`;
    return el("div", { className: "status-pill", attrs: { role: "status", "aria-live": "polite", title: state.statusDetail ?? "" }, children: [el("span", { className: `status-dot ${state.status}` }), el("span", { text: label })] });
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
    if (threadID && !canWrite("selectThread")) {
        state.draft.threadError = t(state.locale, "writeUnavailable");
        state.mobileThreadsOpen = true;
        render();
        return;
    }
    const shouldRestoreThreadButtonFocus = state.mobileThreadsOpen;
    state.draft.targetThreadID = threadID;
    state.mobileThreadsOpen = false;
    persistRoomHistory();
    if (threadID)
        void dispatch({ type: "select_thread", threadID });
    render();
    if (shouldRestoreThreadButtonFocus)
        requestAnimationFrame(() => app.querySelector(".thread-menu-button")?.focus());
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
    const oldList = app.querySelector(".message-list");
    if (oldList && !isScrolledNearBottom(oldList))
        state.unseenCount += 1;
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
    state.liveAnnouncement = `${author}: ${displayDiscordMentions(content, options.mentions ?? [], state.mentionPicker).slice(0, 160)}`;
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
    if (!target)
        return t(state.locale, "targetChannel");
    const channel = target.channelName ? `#${target.channelName}` : target.channelID;
    return target.threadName ? `${channel} / ${target.threadName}` : channel;
}
function draftTargetLabel() {
    const thread = selectedThread();
    if (!thread)
        return targetLabel(state.target);
    const channel = state.target?.channelName ? `#${state.target.channelName}` : state.target?.channelID ?? t(state.locale, "targetChannel");
    return `${channel} / ${thread.name}`;
}
function targetChannelLabel() {
    if (!state.target)
        return t(state.locale, "targetChannel");
    return state.target.channelName ?? state.target.channelID;
}
function serverLabel() {
    if (!state.target)
        return t(state.locale, "appTitle");
    return state.target.guildName || `${t(state.locale, "server")} ${state.target.guildID}`;
}
function serverInitials() {
    const label = serverLabel().trim();
    if (!label)
        return "WS";
    const words = label.split(/\s+/u).filter(Boolean);
    const initials = words.length > 1 ? words.slice(0, 2).map((word) => word[0]).join("") : label.slice(0, 2);
    return initials.toUpperCase();
}
function targetTypeLabel() {
    const type = state.draft.targetThreadID || state.target?.targetType === "thread" ? "thread" : state.target?.targetType;
    if (type === "thread")
        return t(state.locale, "targetTypeThread");
    if (type === "channel")
        return t(state.locale, "targetTypeChannel");
    return type ?? "-";
}
function shareStatusLabel() {
    const status = state.share?.status;
    if (!status)
        return t(state.locale, state.status);
    if (status === "active")
        return t(state.locale, "shareActive");
    if (status === "created")
        return t(state.locale, "shareCreated");
    if (status === "connecting")
        return t(state.locale, "shareConnecting");
    if (status === "disconnected")
        return t(state.locale, "shareDisconnected");
    if (status === "revoked")
        return t(state.locale, "shareRevoked");
    if (status === "degraded")
        return t(state.locale, "permissionLost");
    if (status === "expired")
        return t(state.locale, "shareExpired");
    return status;
}
function shareWritableState() {
    const status = state.share?.status;
    return !status || status === "created" || status === "connecting" || status === "active" || status === "disconnected";
}
function activeTargetLabel() {
    const selected = state.threads.find((thread) => thread.id === state.draft.targetThreadID);
    return selected?.name ?? targetChannelLabel();
}
function composerPlaceholder() {
    if (state.draft.mode === "command")
        return t(state.locale, "placeholderCommand");
    if (state.draft.mode === "message")
        return t(state.locale, "placeholderMessage");
    return t(state.locale, "placeholderAgent");
}
function composerHint(mode) {
    if (mode === "command")
        return t(state.locale, "modeCommand");
    if (mode === "agent" && state.draft.mode === "message" && modeWritable("agent") && draftMentionsBot(state.draft.text, state.mentionPicker.bot))
        return t(state.locale, "modeMentionAgent");
    if (mode === "agent")
        return t(state.locale, "modeAgent");
    return t(state.locale, "modeMessage");
}
function mentionPreview() {
    const names = mentionPreviewNames(state.draft.text, state.mentionPicker, canWrite("mentionUsers"), canWrite("mentionBot"));
    if (names.length === 0)
        return undefined;
    return el("span", { className: "target-chip mention-preview", text: `${t(state.locale, "willPing")}: ${names.join(", ")}` });
}
function terminalMessage(reasonCode) {
    if (reasonCode === "stopped")
        return t(state.locale, "shareStopped");
    if (reasonCode === "revoked")
        return t(state.locale, "shareRevoked");
    if (reasonCode === "permission_lost")
        return t(state.locale, "permissionLost");
    return reasonCode;
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
    if (!hasCapability(event.capabilities, "selectThread"))
        return undefined;
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
        else if (token.kind === "spoiler") {
            const node = el("span", { className: "spoiler", attrs: { tabindex: "0" } });
            node.append(...renderInlineMarkdown(token.content, depth + 1));
            nodes.push(node);
        }
        else {
            const node = document.createElement(token.kind === "strong" ? "strong" : token.kind === "delete" ? "del" : "em");
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
        delimitedToken(text, "||", "spoiler"),
        delimitedToken(text, "~~", "delete"),
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
    return displayDiscordMentions(message.content, message.mentions ?? [], state.mentionPicker);
}
function threadJump(thread) {
    const button = el("button", { className: "thread-jump", text: `${t(state.locale, "openThread")} · ${t(state.locale, "replyInThread")} # ${thread.name}` });
    button.type = "button";
    button.addEventListener("click", () => selectThread(thread.id));
    return button;
}
function createThreadFromMessageButton(message) {
    if (message.kind !== "discord" || !message.discordMessageID || message.threadMessage || message.deleted || !canWrite("createThread"))
        return undefined;
    const button = el("button", { className: "message-action", text: t(state.locale, "createThreadFromMessage"), attrs: { type: "button", "aria-label": t(state.locale, "createThreadFromMessage") } });
    button.addEventListener("click", () => {
        state.draft.sourceMessageID = message.discordMessageID ?? "";
        state.draft.threadError = undefined;
        if (!state.draft.newThreadName.trim())
            state.draft.newThreadName = suggestedThreadName(message.author, displayMessageContent(message));
        state.mobileThreadsOpen = true;
        render();
        requestAnimationFrame(() => {
            const mobileInput = app.querySelector(".mobile-thread-panel .thread-create input");
            if (mobileInput && mobileInput.offsetParent !== null) {
                mobileInput.focus();
                return;
            }
            app.querySelector(".channel-sidebar .thread-create input")?.focus();
        });
    });
    return button;
}
function replyPreview(replyTo) {
    const author = replyTo.author?.displayName ?? t(state.locale, "repliedMessage");
    const content = replyTo.deleted ? t(state.locale, "messageDeleted") : displayDiscordMentions(replyTo.content ?? replyTo.messageID, replyTo.mentions ?? [], state.mentionPicker);
    return el("div", { className: "reply-preview", text: `${t(state.locale, "replyingTo")} ${author}: ${content.slice(0, 160)}` });
}
