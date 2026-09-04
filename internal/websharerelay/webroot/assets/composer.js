export function resolveDraftMode(selectedMode, text, bot) {
    const trimmed = text.trimStart();
    if (trimmed.startsWith("/") && selectedMode !== "command")
        return "command";
    if (selectedMode === "message" && draftMentionsBot(text, bot))
        return "agent";
    return selectedMode;
}
export function commandName(command) {
    return command.trim().split(/\s+/u)[0]?.toLowerCase() ?? "";
}
export function highRiskCommand(name) {
    switch (name) {
        case "reset":
        case "restart":
        case "cancel":
        case "clear":
        case "close":
        case "close-thread":
        case "interrupt":
        case "webhook":
            return true;
        default:
            return false;
    }
}
export function webshareCommandAllowed(name) {
    switch (name) {
        case "cron-list":
        case "cron-run":
        case "remind":
        case "usage-history":
            return true;
        default:
            return false;
    }
}
export function draftMentionsBot(text, bot) {
    if (!bot)
        return false;
    const normalized = text.toLowerCase();
    return normalized.includes(`<@${bot.id}>`) || normalized.includes(`<@!${bot.id}>`);
}
export function draftMentionsUser(text, userID) {
    return text.includes(`<@${userID}>`) || text.includes(`<@!${userID}>`) || text.includes(`[[discord:user:${userID}]]`);
}
export function allowedMentionSelectionForDraft(text, picker, canMentionUsers, canMentionBot) {
    const users = canMentionUsers ? [...picker.selectedUsers].filter((userID) => draftMentionsUser(text, userID)) : [];
    const bot = canMentionBot && picker.botSelected && draftMentionsBot(text, picker.bot);
    return bot ? { users, bot: true } : { users };
}
export function mentionPreviewNames(text, picker, canMentionUsers, canMentionBot) {
    const names = [];
    const bot = picker.bot;
    if (bot && canMentionBot && picker.botSelected && draftMentionsBot(text, bot))
        names.push(`@${bot.displayName}`);
    if (canMentionUsers) {
        for (const user of picker.users) {
            if (!picker.selectedUsers.has(user.id))
                continue;
            if (!draftMentionsUser(text, user.id))
                continue;
            names.push(`@${user.displayName}`);
        }
    }
    return names;
}
export function displayDiscordMentions(text, mentions = [], picker) {
    if (!text)
        return "";
    const lookup = new Map();
    for (const mention of mentions)
        lookup.set(mention.id, mention);
    if (picker?.bot)
        lookup.set(picker.bot.id, { ...picker.bot, bot: true, kind: "bot" });
    for (const user of picker?.users ?? [])
        lookup.set(user.id, { ...user, kind: "user" });
    const label = (kind, id, fallbackPrefix) => {
        const mention = lookup.get(id);
        if (mention) {
            if (mention.kind === "channel" || kind === "channel")
                return `#${mention.displayName}`;
            return `@${mention.displayName}`;
        }
        if (kind === "channel")
            return "#channel";
        if (kind === "role")
            return "@role";
        return fallbackPrefix;
    };
    return text
        .replace(/\[\[discord:(user|role):(\d+)\]\]/g, (_token, kind, id) => label(kind, id, kind === "role" ? "@role" : "@Discord user"))
        .replace(/<(@!?|@&|#)\u200b?(\d+)>/g, (_token, prefix, id) => {
        if (prefix === "#")
            return label("channel", id, "#channel");
        if (prefix === "@&")
            return label("role", id, "@role");
        return label("user", id, "@Discord user");
    });
}
