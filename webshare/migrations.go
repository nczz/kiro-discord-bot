package webshare

const webshareSchema = `
CREATE TABLE IF NOT EXISTS webshare_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  share_id TEXT NOT NULL UNIQUE,
  guild_id TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  parent_channel_id TEXT NOT NULL DEFAULT '',
  opener_user_id TEXT NOT NULL,
  opener_username TEXT NOT NULL DEFAULT '',
  relay_url TEXT NOT NULL,
  public_base_url TEXT NOT NULL,
  room_id TEXT NOT NULL UNIQUE,
  room_key_ciphertext BLOB NOT NULL,
  write_token_hash BLOB NOT NULL,
  view_secret_fingerprint TEXT NOT NULL DEFAULT '',
  capabilities_json TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_connected_at TEXT NOT NULL DEFAULT '',
  last_peer_seen_at TEXT NOT NULL DEFAULT '',
  revoked_at TEXT NOT NULL DEFAULT '',
  revoked_by_user_id TEXT NOT NULL DEFAULT '',
  revoke_reason TEXT NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS webshare_active_opener_target
ON webshare_sessions(guild_id, target_type, target_id, opener_user_id)
WHERE status IN ('created','connecting','active','disconnected');

CREATE TABLE IF NOT EXISTS webshare_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  share_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  actor_user_id TEXT NOT NULL DEFAULT '',
  remote_actor_name TEXT NOT NULL DEFAULT '',
  target_id TEXT NOT NULL DEFAULT '',
  allowed INTEGER NOT NULL,
  reason_code TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS webshare_events_share_created ON webshare_events(share_id, created_at);

CREATE TABLE IF NOT EXISTS webshare_managed_child_threads (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  share_id TEXT NOT NULL,
  parent_channel_id TEXT NOT NULL,
  thread_id TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  created_by_user_id TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE(share_id, thread_id)
);

CREATE INDEX IF NOT EXISTS webshare_child_threads_parent ON webshare_managed_child_threads(share_id, parent_channel_id);

CREATE TABLE IF NOT EXISTS webshare_attachment_refs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ref_id TEXT NOT NULL UNIQUE,
  share_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  message_id TEXT NOT NULL,
  attachment_id TEXT NOT NULL,
  filename TEXT NOT NULL,
  size INTEGER NOT NULL,
  content_type TEXT NOT NULL DEFAULT '',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS webshare_attachment_refs_scope ON webshare_attachment_refs(share_id, ref_id, expires_at);
`
