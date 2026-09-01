package webshare

const (
	EventShareCreated       = "webshare_created"
	EventConnected          = "webshare_connected"
	EventActionRequested    = "webshare_action_requested"
	EventActionRejected     = "webshare_action_rejected"
	EventUploadCompleted    = "webshare_upload_completed"
	EventAttachmentFetched  = "webshare_attachment_fetched"
	EventInterrupted        = "webshare_interrupted"
	EventDisconnected       = "webshare_disconnected"
	EventRevoked            = "webshare_revoked"
)

func NewAuditMetadata(share Share, action string, allowed bool, reasonCode string) AuditMetadata {
	return AuditMetadata{
		"share_id": share.ShareID,
		"guild_id": share.GuildID,
		"target_type": string(share.TargetType),
		"target_id": share.TargetID,
		"opener_user_id": share.OpenerUserID,
		"action": action,
		"allowed": allowed,
		"reason_code": reasonCode,
	}
}
