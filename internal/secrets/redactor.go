package secrets

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const placeholder = "[REDACTED]"

var sensitiveKeyPattern = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|credential|authorization)`)

var assignmentPattern = regexp.MustCompile(`(?i)\b([A-Z0-9_.-]*(?:API[_-]?KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL)[A-Z0-9_.-]*)\s*[:=]\s*("[^"\r\n]*"|'[^'\r\n]*'|[^\s\r\n]+)`)
var jsonSecretPattern = regexp.MustCompile(`(?i)("?(?:api[_-]?key|token|secret|password|credential)"?\s*:\s*)("[^"\r\n]*"|[^\s,}\r\n]+)`)
var bearerPattern = regexp.MustCompile(`(?i)\b(authorization\s*:\s*bearer\s+|bearer\s+)([A-Za-z0-9._~+/=-]{8,})`)

// Redactor replaces known secret values and common secret assignments in text.
type Redactor struct {
	values []knownValue
}

type knownValue struct {
	name  string
	value string
}

var defaultRedactor struct {
	once sync.Once
	r    *Redactor
}

// FromEnv builds a redactor from sensitive environment variables in the current process.
func FromEnv() *Redactor {
	r := &Redactor{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok || !looksSensitiveKey(key) {
			continue
		}
		r.Add(key, value)
	}
	return r
}

// RedactEnv redacts text with the process-wide environment-backed redactor.
func RedactEnv(text string) string {
	defaultRedactor.once.Do(func() {
		defaultRedactor.r = FromEnv()
	})
	return defaultRedactor.r.Redact(text)
}

func looksSensitiveKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	return sensitiveKeyPattern.MatchString(key)
}

// Add registers one exact secret value. Short values are ignored to avoid over-redaction.
func (r *Redactor) Add(name, value string) {
	value = strings.TrimSpace(value)
	if len(value) < 8 {
		return
	}
	r.values = append(r.values, knownValue{name: strings.TrimSpace(name), value: value})
	sort.SliceStable(r.values, func(i, j int) bool {
		return len(r.values[i].value) > len(r.values[j].value)
	})
}

// Redact returns text with known values and common secret assignments replaced.
func (r *Redactor) Redact(text string) string {
	if text == "" {
		return ""
	}
	out := text
	if r != nil {
		for _, item := range r.values {
			label := placeholder
			if item.name != "" {
				label = "[REDACTED:" + item.name + "]"
			}
			out = strings.ReplaceAll(out, item.value, label)
		}
	}
	out = assignmentPattern.ReplaceAllString(out, `${1}=`+placeholder)
	out = jsonSecretPattern.ReplaceAllString(out, `${1}"`+placeholder+`"`)
	out = bearerPattern.ReplaceAllString(out, `${1}`+placeholder)
	return out
}
