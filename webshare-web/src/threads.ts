import { el } from "./dom.js";
import type { ClientAction, ThreadView } from "./protocol.js";
import type { Locale } from "./i18n.js";
import { t } from "./i18n.js";

export interface ThreadPanelState {
  threads: ThreadView[];
  selectedThreadID: string | undefined;
  canCreate: boolean;
  canSelect: boolean;
}

export function createThreadPanel(locale: Locale, state: ThreadPanelState, dispatch: (action: ClientAction) => void): HTMLElement {
  const root = el("div", { className: "card stack" });
  root.append(el("h2", { text: t(locale, "threads") }));

  const select = el("select") as HTMLSelectElement;
  select.append(new Option(t(locale, "targetChannel"), ""));
  for (const thread of state.threads) select.append(new Option(thread.name, thread.id, false, thread.id === state.selectedThreadID));
  select.value = state.selectedThreadID ?? "";
  select.disabled = !state.canSelect;
  const selectButton = el("button", { text: t(locale, "selectThread") }) as HTMLButtonElement;
  selectButton.disabled = !state.canSelect;
  selectButton.addEventListener("click", () => {
    if (select.value) dispatch({ type: "select_thread", threadID: select.value });
  });
  const current = state.threads.find((thread) => thread.id === state.selectedThreadID)?.name ?? t(locale, "targetChannel");
  root.append(label(t(locale, "currentThread"), el("div", { className: "muted", text: current })));
  root.append(label(t(locale, "selectThread"), select));
  root.append(selectButton);

  const nameInput = el("input", { attrs: { placeholder: t(locale, "threadName") } }) as HTMLInputElement;
  const sourceInput = el("input", { attrs: { placeholder: t(locale, "sourceMessage") } }) as HTMLInputElement;
  const createButton = el("button", { text: t(locale, "createThread") }) as HTMLButtonElement;
  createButton.disabled = !state.canCreate;
  createButton.addEventListener("click", () => {
    const name = nameInput.value.trim();
    if (!name) return;
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

function label(text: string, child: HTMLElement): HTMLLabelElement {
  const node = el("label");
  node.append(document.createTextNode(text), child);
  return node;
}
