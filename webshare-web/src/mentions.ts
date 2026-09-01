import { el } from "./dom.js";
import type { AllowedMentionSelection, MentionableBot, MentionableUser } from "./protocol.js";
import type { Locale } from "./i18n.js";
import { t } from "./i18n.js";

export interface MentionPickerState {
  users: MentionableUser[];
  bot: MentionableBot | undefined;
  selectedUsers: Set<string>;
  botSelected: boolean;
}

export function createMentionPicker(locale: Locale, state: MentionPickerState, onChange: () => void): HTMLElement {
  const root = el("div", { className: "stack" });
  root.append(el("h3", { text: t(locale, "mentions") }));
  root.append(el("p", { className: "muted", text: t(locale, "mentionHelp") }));
  root.append(el("p", { className: "error-text", text: t(locale, "noMentionEveryone") }));
  const list = el("div", { className: "check-list" });

  if (state.bot) {
    const checkbox = el("input", { attrs: { type: "checkbox" } }) as HTMLInputElement;
    checkbox.checked = state.botSelected;
    checkbox.addEventListener("change", () => {
      state.botSelected = checkbox.checked;
      onChange();
    });
    list.append(labelWithInput(checkbox, `${state.bot.displayName} (bot)`));
  }

  for (const user of state.users) {
    const checkbox = el("input", { attrs: { type: "checkbox" } }) as HTMLInputElement;
    checkbox.checked = state.selectedUsers.has(user.id);
    checkbox.addEventListener("change", () => {
      if (checkbox.checked) state.selectedUsers.add(user.id);
      else state.selectedUsers.delete(user.id);
      onChange();
    });
    list.append(labelWithInput(checkbox, user.displayName));
  }

  root.append(list);
  return root;
}

export function mentionSelection(state: MentionPickerState): AllowedMentionSelection {
  return state.botSelected ? { users: [...state.selectedUsers], bot: true } : { users: [...state.selectedUsers] };
}

function labelWithInput(input: HTMLInputElement, text: string): HTMLLabelElement {
  const label = el("label");
  label.append(input, document.createTextNode(text));
  return label;
}
