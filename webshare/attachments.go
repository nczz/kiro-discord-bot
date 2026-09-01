package webshare

import (
	"path/filepath"
	"strings"
	"unicode"
)

func SafeAttachmentFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	return capRunes(name, 128)
}

func UploadDir(projectCWD, shareID, uploadID string) string {
	return filepath.Join(projectCWD, ".kiro-bot", "attachments", "webshare-"+shareID, uploadID)
}
