export const PROTOCOL_VERSION = 1 as const;

export type Capability =
  | "view"
  | "write"
  | "sendAgentPrompt"
  | "postChannelMessage"
  | "runBotCommand"
  | "createThread"
  | "selectThread"
  | "interruptAgent"
  | "upload"
  | "fetchAttachment"
  | "mentionUsers"
  | "mentionBot";

export type Capabilities = Partial<Record<Capability, boolean>> & {
  maxUploadBytes?: number;
  maxAttachmentFetchBytes?: number;
  maxMessageBytes?: number;
};

export interface AttachmentRef {
  attachmentRef: string;
  ref?: string;
  filename: string;
  size: number;
  mime?: string;
}

export interface AttachmentMetadata {
  streamID?: string;
  attachmentRef?: string;
  filename: string;
  size: number;
  mime?: string;
  sha256?: string;
}

export interface MentionableUser {
  id: string;
  displayName: string;
  username?: string;
  avatarURL?: string;
  recent?: boolean;
}

export interface MentionableBot {
  id: string;
  displayName: string;
}
export interface MentionView {
  id: string;
  displayName: string;
  username?: string;
  bot?: boolean;
  kind?: "user" | "bot" | "role" | "channel" | string;
}
export interface MessageReferenceView {
  messageID: string;
  channelID?: string;
  guildID?: string;
  author?: ActorView;
  content?: string;
  deleted?: boolean;
}



export interface AllowedMentionSelection {
  users: string[];
  bot?: boolean;
}

export interface ShareView {
  id?: string;
  roomID: string;
  status: "active" | "revoked" | "expired" | "degraded" | string;
  mode?: "view" | "write";
  createdAt?: string;
}

export interface TargetView {
  guildID?: string;
  guildName?: string;
  channelID: string;
  channelName?: string;
  threadID?: string;
  threadName?: string;
  targetType?: "channel" | "thread" | string;
}

export interface ActorView {
  id: string;
  displayName: string;
  username?: string;
}

export interface ThreadView {
  id: string;
  name: string;
  parentChannelID?: string;
  archived?: boolean;
  selected?: boolean;
}

export interface SanitizedDiscordAttachment {
  attachmentRef: string;
  filename: string;
  size: number;
  mime?: string;
}

export interface SanitizedDiscordEvent {
  eventID: string;
  timestamp?: string;
  messageID?: string;
  author?: ActorView;
  content?: string;
  attachments?: SanitizedDiscordAttachment[];
  mentionableUsers?: MentionableUser[];
  mentionableBot?: MentionableBot;
  mentions?: MentionView[];
  action?: "created" | "updated" | "deleted" | string;
  replyTo?: MessageReferenceView;
}

export interface SanitizedThreadEvent {
  eventID: string;
  timestamp?: string;
  messageID?: string;
  thread: ThreadView;
  action: "created" | "selected" | "updated" | "message" | string;
  content?: string;
  author?: ActorView;
  attachments?: SanitizedDiscordAttachment[];
  mentions?: MentionView[];
  replyTo?: MessageReferenceView;
}

export interface SanitizedAgentEvent {
  eventID: string;
  timestamp?: string;
  jobID?: string;
  status: "queued" | "running" | "final" | "error" | "interrupted" | string;
  content?: string;
}
export type ClientAction = (
  | { type: "hello"; proto: typeof PROTOCOL_VERSION; displayName: string }
  | { type: "send_agent_prompt"; text: string; attachments?: AttachmentRef[]; targetThreadID?: string; allowedMentions?: AllowedMentionSelection }
  | { type: "post_channel_message"; text: string; attachments?: AttachmentRef[]; targetThreadID?: string; allowedMentions: AllowedMentionSelection }
  | { type: "run_bot_command"; command: string; args: Record<string, unknown>; targetThreadID?: string }
  | { type: "create_thread"; sourceMessageID?: string; name: string; autoArchiveDuration?: number }
  | { type: "select_thread"; threadID: string }
  | { type: "interrupt_agent"; jobID?: string }
  | { type: "upload_init"; uploadID?: string; name: string; mime: string; size: number; sha256?: string }
  | { type: "upload_chunk"; uploadID: string; seq: number; bytes: string }
  | { type: "upload_finish"; uploadID: string }
  | { type: "fetch_discord_attachment"; attachmentRef: string }
) & { writeToken?: string };

export type ServerEvent =
  | { type: "welcome"; share: ShareView; target: TargetView; opener: ActorView; capabilities: Capabilities; mentionableUsers?: MentionableUser[]; mentionableBot?: MentionableBot; threads?: ThreadView[]; selectedThreadID?: string }
  | { type: "channel_event"; event: SanitizedDiscordEvent }
  | { type: "thread_event"; event: SanitizedThreadEvent }
  | { type: "agent_event"; event: SanitizedAgentEvent }
  | { type: "command_result"; requestID?: string; status: "ok" | "error" | "rejected"; content: string; visibility?: "web" | "discord" | "both" }
  | { type: "upload_state"; uploadID: string; status: "accepted" | "received" | "complete" | "rejected"; reasonCode?: string; reason?: string; metadata?: AttachmentMetadata & { attachmentRef?: string } }
  | { type: "attachment_stream"; streamID: string; metadata: AttachmentMetadata; chunk?: string; done?: boolean }
  | { type: "notice"; level: "info" | "warn"; messageKey: string; args?: string[] }
  | { type: "error"; code?: string; messageKey?: string; reasonCode?: string; content?: string; args?: string[] }
  | { type: "bye"; reasonCode: string };

export function hasCapability(capabilities: Capabilities | undefined, capability: Capability): boolean {
  return Boolean(capabilities?.[capability]);
}
