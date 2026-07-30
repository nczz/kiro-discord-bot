package a2a

const (
	ErrorUnsupportedOperation ErrorCode = "unsupported_operation"
	ErrorInvalidSubject       ErrorCode = "invalid_subject"
	ErrorInvalidEnvelope      ErrorCode = "invalid_envelope"
	ErrorPolicyDenied         ErrorCode = "policy_denied"
	ErrorCapabilityDenied     ErrorCode = "capability_denied"
	ErrorTimeout              ErrorCode = "timeout"
	ErrorInternal             ErrorCode = "internal"
)
