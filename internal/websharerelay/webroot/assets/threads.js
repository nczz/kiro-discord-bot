import { el } from "./dom.js";
import { t } from "./i18n.js";
export function createThreadPanel(locale, state, dispatch) {
    const root = el("div", { className: "card stack" });
    root.append(el("h2", { text: t(locale, "threads") }));
    const select = el("select");
    select.append(new Option(t(locale, "targetChannel"), ""));
    for (const thread of state.threads)
        select.append(new Option(thread.name, thread.id, false, thread.id === state.selectedThreadID));
    select.value = state.selectedThreadID ?? "";
    select.disabled = !state.canSelect;
    const selectButton = el("button", { text: t(locale, "selectThread") });
    selectButton.disabled = !state.canSelect;
    selectButton.addEventListener("click", () => {
        if (select.value)
            dispatch({ type: "select_thread", threadID: select.value });
    });
    const current = state.threads.find((thread) => thread.id === state.selectedThreadID)?.name ?? t(locale, "targetChannel");
    root.append(label(t(locale, "currentThread"), el("div", { className: "muted", text: current })));
    root.append(label(t(locale, "selectThread"), select));
    root.append(selectButton);
    const nameInput = el("input", { attrs: { placeholder: t(locale, "threadName") } });
    const sourceInput = el("input", { attrs: { placeholder: t(locale, "sourceMessage") } });
    const createButton = el("button", { text: t(locale, "createThread") });
    createButton.disabled = !state.canCreate;
    createButton.addEventListener("click", () => {
        const name = nameInput.value.trim();
        if (!name)
            return;
        const sourceMessageID = sourceInput.value.trim();
        dispatch({ type: "create_thread", name, ...(sourceMessageID ? { sourceMessageID } : {}) });
        nameInput.value = "";
        sourceInput.value = "";
    });
    root.append(label(t(locale, "threadName"), nameInput));
    root.append(label(t(locale, "sourceMessage"), sourceInput));
    root.append(createButton);
    return root;
}
function label(text, child) {
    const node = el("label");
    node.append(document.createTextNode(text), child);
    return node;
}
