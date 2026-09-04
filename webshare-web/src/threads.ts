const discordMessageURL = /(?:https?:\/\/)?(?:ptb\.|canary\.)?discord(?:app)?\.com\/channels\/\d+\/\d+\/(\d{15,25})(?:\b|$)/u;
const discordSnowflake = /^\d{15,25}$/u;

export function parseDiscordMessageReference(input: string): string | undefined {
  const trimmed = input.trim();
  if (!trimmed) return undefined;
  if (discordSnowflake.test(trimmed)) return trimmed;
  return discordMessageURL.exec(trimmed)?.[1];
}

export function suggestedThreadName(author: string, content: string): string {
  const normalized = content.replace(/\s+/gu, " ").trim();
  if (normalized) return normalized.slice(0, 60);
  const actor = author.trim();
  return actor ? `Thread with ${actor}` : "WebShare thread";
}
