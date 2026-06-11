package textutil

import "unicode/utf8"

// TruncateUTF8Bytes returns a prefix no longer than maxBytes without splitting a UTF-8 rune.
func TruncateUTF8Bytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
