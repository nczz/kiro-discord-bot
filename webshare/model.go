package webshare

import "time"

// Status is the authoritative lifecycle state for a delegated WebShare session.
type Status string

const (
	StatusCreated      Status = "created"
	StatusConnecting   Status = "connecting"
	StatusActive       Status = "active"
	StatusDisconnected Status = "disconnected"
	StatusRevoked      Status = "revoked"
	StatusDegraded     Status = "degraded"
)

func (s Status) ActiveLocking() bool {
	switch s {
	case StatusCreated, StatusConnecting, StatusActive, StatusDisconnected:
		return true
	default:
		return false
	}
}

// TargetType names the Discord target kind bound to a share.
type TargetType string

const (
	TargetChannel TargetType = "channel"
	TargetThread  TargetType = "thread"
)

// Capabilities are granted by the opener's link and still require per-action
// Discord permission checks by bot integrations.
type Capabilities struct {
	View               bool `json:"view"`
	Write              bool `json:"write"`
	SendAgentPrompt    bool `json:"sendAgentPrompt"`
	PostChannelMessage bool `json:"postChannelMessage"`
	RunBotCommand      bool `json:"runBotCommand"`
	CreateThread       bool `json:"createThread"`
	SelectThread       bool `json:"selectThread"`
	InterruptAgent     bool `json:"interruptAgent"`
	Upload             bool `json:"upload"`
	FetchAttachment    bool `json:"fetchAttachment"`
	MentionUsers       bool `json:"mentionUsers"`
	MentionBot         bool `json:"mentionBot"`
	MentionRoles       bool `json:"mentionRoles"`
	MentionEveryone    bool `json:"mentionEveryone"`
	MentionHere        bool `json:"mentionHere"`
}

func ViewCapabilities() Capabilities { return Capabilities{View: true} }

func WriteCapabilities() Capabilities {
	return Capabilities{
		View:               true,
		Write:              true,
		SendAgentPrompt:    true,
		PostChannelMessage: true,
		RunBotCommand:      true,
		CreateThread:       true,
		SelectThread:       true,
		InterruptAgent:     true,
		Upload:             true,
		FetchAttachment:    true,
		MentionUsers:       true,
		MentionBot:         true,
		MentionRoles:       false,
		MentionEveryone:    false,
		MentionHere:        false,
	}
}

func CapabilitiesForWrite(canWrite bool) Capabilities {
	if canWrite {
		return WriteCapabilities()
	}
	return ViewCapabilities()
}

// Links are capability URLs returned only to the opener. They must not be logged
// or stored because the URL fragment contains key material.
type Links struct {
	View  string `json:"view"`
	Write string `json:"write,omitempty"`
}

// Share is the store-facing representation of a target-scoped delegated session.
type Share struct {
	ID                    int64        `json:"-"`
	ShareID               string       `json:"shareID"`
	GuildID               string       `json:"guildID"`
	TargetType            TargetType   `json:"targetType"`
	TargetID              string       `json:"targetID"`
	ParentChannelID       string       `json:"parentChannelID,omitempty"`
	OpenerUserID          string       `json:"openerUserID"`
	OpenerUsername        string       `json:"openerUsername,omitempty"`
	RelayURL              string       `json:"relayURL"`
	PublicBaseURL         string       `json:"publicBaseURL"`
	RoomID                string       `json:"roomID"`
	RoomKeyCiphertext     []byte       `json:"-"`
	WriteTokenHash        []byte       `json:"-"`
	ViewSecretFingerprint string       `json:"viewSecretFingerprint,omitempty"`
	Capabilities          Capabilities `json:"capabilities"`
	Status                Status       `json:"status"`
	CreatedAt             time.Time    `json:"createdAt"`
	UpdatedAt             time.Time    `json:"updatedAt"`
	LastConnectedAt       time.Time    `json:"lastConnectedAt,omitempty"`
	LastPeerSeenAt        time.Time    `json:"lastPeerSeenAt,omitempty"`
	RevokedAt             time.Time    `json:"revokedAt,omitempty"`
	RevokedByUserID       string       `json:"revokedByUserID,omitempty"`
	RevokeReason          string       `json:"revokeReason,omitempty"`
}

type Peer struct {
	ID          uint32    `json:"id"`
	DisplayName string    `json:"displayName"`
	CanWrite    bool      `json:"canWrite"`
	ConnectedAt time.Time `json:"connectedAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

type Event struct {
	ID              int64          `json:"id"`
	ShareID         string         `json:"shareID"`
	Type            string         `json:"type"`
	ActorUserID     string         `json:"actorUserID,omitempty"`
	RemoteActorName string         `json:"remoteActorName,omitempty"`
	TargetID        string         `json:"targetID,omitempty"`
	Allowed         bool           `json:"allowed"`
	ReasonCode      string         `json:"reasonCode,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
}

type AuditMetadata map[string]any

type ManagedChildThread struct {
	ID              int64          `json:"-"`
	ShareID         string         `json:"shareID"`
	ParentChannelID string         `json:"parentChannelID"`
	ThreadID        string         `json:"threadID"`
	Name            string         `json:"name,omitempty"`
	CreatedByUserID string         `json:"createdByUserID,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
}

type AttachmentRef struct {
	ID           string         `json:"ref"`
	ShareID      string         `json:"shareID"`
	TargetID     string         `json:"targetID"`
	MessageID    string         `json:"messageID"`
	AttachmentID string         `json:"attachmentID"`
	Filename     string         `json:"filename"`
	Size         int64          `json:"size"`
	ContentType  string         `json:"contentType,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	ExpiresAt    time.Time      `json:"expiresAt"`
	CreatedAt    time.Time      `json:"createdAt"`
}

type AllowedMentionSelection struct {
	Users       []string `json:"users,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	RepliedUser bool     `json:"repliedUser"`
	Everyone    bool     `json:"everyone"`
	Here        bool     `json:"here"`
	Bot         bool     `json:"bot"`
}

type ClientAction struct {
	Type                string                  `json:"type"`
	Proto               int                     `json:"proto,omitempty"`
	DisplayName         string                  `json:"displayName,omitempty"`
	WriteToken          string                  `json:"writeToken,omitempty"`
	Text                string                  `json:"text,omitempty"`
	Attachments         []AttachmentRef         `json:"attachments,omitempty"`
	TargetThreadID      string                  `json:"targetThreadID,omitempty"`
	AllowedMentions     AllowedMentionSelection `json:"allowedMentions,omitempty"`
	Command             string                  `json:"command,omitempty"`
	Args                map[string]any          `json:"args,omitempty"`
	SourceMessageID     string                  `json:"sourceMessageID,omitempty"`
	Name                string                  `json:"name,omitempty"`
	AutoArchiveDuration int                     `json:"autoArchiveDuration,omitempty"`
	ThreadID            string                  `json:"threadID,omitempty"`
	JobID               string                  `json:"jobID,omitempty"`
	UploadID            string                  `json:"uploadID,omitempty"`
	Seq                 uint64                  `json:"seq,omitempty"`
	Bytes               string                  `json:"bytes,omitempty"`
	MIME                string                  `json:"mime,omitempty"`
	Size                int64                   `json:"size,omitempty"`
	SHA256              string                  `json:"sha256,omitempty"`
	AttachmentRef       string                  `json:"attachmentRef,omitempty"`
	EventID             string                  `json:"eventID,omitempty"`
}

type ServerEvent struct {
	Type             string         `json:"type"`
	EventID          string         `json:"eventID,omitempty"`
	Share            any            `json:"share,omitempty"`
	Target           any            `json:"target,omitempty"`
	Opener           any            `json:"opener,omitempty"`
	Capabilities     Capabilities   `json:"capabilities,omitempty"`
	MentionableUsers any            `json:"mentionableUsers,omitempty"`
	MentionableBot   any            `json:"mentionableBot,omitempty"`
	Threads          any            `json:"threads,omitempty"`
	SelectedThreadID string         `json:"selectedThreadID,omitempty"`
	Event            any            `json:"event,omitempty"`
	RequestID        string         `json:"requestID,omitempty"`
	Status           string         `json:"status,omitempty"`
	Content          string         `json:"content,omitempty"`
	Visibility       string         `json:"visibility,omitempty"`
	UploadID         string         `json:"uploadID,omitempty"`
	Reason           string         `json:"reason,omitempty"`
	StreamID         string         `json:"streamID,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	Chunk            string         `json:"chunk,omitempty"`
	Done             bool           `json:"done,omitempty"`
	Level            string         `json:"level,omitempty"`
	MessageKey       string         `json:"messageKey,omitempty"`
	Args             []string       `json:"args,omitempty"`
	Code             string         `json:"code,omitempty"`
	ReasonCode       string         `json:"reasonCode,omitempty"`
}
