package botegress

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// ErrUnsupportedExtractableFormat is returned when a binary file cannot be
// safely converted into redactable plain text.
var ErrUnsupportedExtractableFormat = errors.New("unsupported extractable format")

// ErrExtractFailed is returned when a supported document cannot be converted
// into redactable plain text.
var ErrExtractFailed = errors.New("extract readable text")

// extractableFormat identifies formats that can be converted to plain text for
// safe egress. The sanitized upload is always a .txt copy; the original binary
// is never uploaded back to Discord.
type extractableFormat int

const (
	formatUnknown extractableFormat = iota
	formatPDF
	formatDOCX
	formatXLSX
)

// ExtractResult holds extracted plain text metadata.
type ExtractResult struct {
	Text   string
	Format string // "pdf", "docx", "xlsx"
}

var pdfLiteralStringPattern = regexp.MustCompile(`\((?:\\.|[^\\()])*\)`)
var pdfHexStringPattern = regexp.MustCompile(`<([0-9A-Fa-f\s]{2,})>`)
var pdfStreamPattern = regexp.MustCompile(`(?s)(<<.*?/FlateDecode.*?>>)\s*stream\r?\n(.*?)\r?\nendstream`)

const maxExtractedTextBytes = MaxSanitizableFileBytes

// DetectFormat inspects the file extension and optional magic bytes to classify
// a document format supported for extract-to-text safe egress.
func DetectFormat(path string, raw []byte) extractableFormat {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pdf":
		return formatPDF
	case ".docx":
		return formatDOCX
	case ".xlsx":
		return formatXLSX
	}
	if len(raw) >= 5 && string(raw[:5]) == "%PDF-" {
		return formatPDF
	}
	return formatUnknown
}

// ExtractFile reads a supported document and returns redactable plain text.
// Callers are responsible for redacting the returned text and writing it to the
// sanitized output path.
func ExtractFile(path string) (ExtractResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("read file: %w", err)
	}
	return ExtractText(path, raw)
}

// ExtractText converts a supported document into plain text. It intentionally
// does not attempt original-format rewriting because safe egress prioritizes a
// verifiable sanitized text copy over document fidelity.
func ExtractText(path string, raw []byte) (ExtractResult, error) {
	fmtType := DetectFormat(path, raw)
	if fmtType == formatUnknown {
		return ExtractResult{}, ErrUnsupportedExtractableFormat
	}

	var (
		text string
		err  error
	)
	switch fmtType {
	case formatPDF:
		text, err = extractPDFText(raw)
	case formatDOCX:
		text, err = extractDOCXText(raw)
	case formatXLSX:
		text, err = extractXLSXText(raw)
	}
	if err != nil {
		return ExtractResult{}, fmt.Errorf("%w: %v", ErrExtractFailed, err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ExtractResult{}, fmt.Errorf("%w: no readable text found", ErrExtractFailed)
	}
	return ExtractResult{Text: text, Format: formatName(fmtType)}, nil
}

func formatName(fmtType extractableFormat) string {
	switch fmtType {
	case formatPDF:
		return "pdf"
	case formatDOCX:
		return "docx"
	case formatXLSX:
		return "xlsx"
	default:
		return "unknown"
	}
}

func extractPDFText(raw []byte) (string, error) {
	if len(raw) < 5 || string(raw[:5]) != "%PDF-" {
		return "", fmt.Errorf("missing PDF header")
	}
	acc := newExtractedTextAccumulator(maxExtractedTextBytes)
	if err := appendPDFTextStrings(acc, raw); err != nil {
		return "", err
	}
	for _, match := range pdfStreamPattern.FindAllSubmatch(raw, -1) {
		decompressed, err := inflatePDFStream(match[2])
		if err != nil {
			continue
		}
		if err := appendPDFTextStrings(acc, decompressed); err != nil {
			return "", err
		}
	}
	return acc.String(), nil
}

func appendPDFTextStrings(acc *extractedTextAccumulator, raw []byte) error {
	asText := string(raw)
	for _, match := range pdfLiteralStringPattern.FindAllString(asText, -1) {
		text := unescapePDFLiteral(match[1 : len(match)-1])
		if strings.TrimSpace(text) != "" {
			if err := acc.AddLine(text); err != nil {
				return err
			}
		}
	}
	for _, match := range pdfHexStringPattern.FindAllStringSubmatch(asText, -1) {
		text := decodePDFHexString(match[1])
		if strings.TrimSpace(text) != "" {
			if err := acc.AddLine(text); err != nil {
				return err
			}
		}
	}
	return nil
}

func inflatePDFStream(raw []byte) ([]byte, error) {
	if int64(len(raw)) > maxExtractedTextBytes {
		return nil, fmt.Errorf("compressed PDF stream exceeds extract limit")
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return readLimited(zr, maxExtractedTextBytes)
}

func unescapePDFLiteral(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i == len(s)-1 {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case '(', ')', '\\':
			b.WriteByte(s[i])
		case '\n':
			// Line continuation; omit both characters.
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		default:
			if s[i] >= '0' && s[i] <= '7' {
				start := i
				for i+1 < len(s) && i-start < 2 && s[i+1] >= '0' && s[i+1] <= '7' {
					i++
				}
				var value byte
				for _, c := range s[start : i+1] {
					value = value*8 + byte(c-'0')
				}
				b.WriteByte(value)
			} else {
				b.WriteByte(s[i])
			}
		}
	}
	return b.String()
}

func decodePDFHexString(s string) string {
	compact := strings.Join(strings.Fields(s), "")
	if len(compact)%2 == 1 {
		compact += "0"
	}
	data, err := hex.DecodeString(compact)
	if err != nil || len(data) == 0 {
		return ""
	}
	if len(data) >= 2 {
		if data[0] == 0xfe && data[1] == 0xff {
			return decodeUTF16BE(data[2:])
		}
		if data[0] == 0xff && data[1] == 0xfe {
			return decodeUTF16LE(data[2:])
		}
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return string(bytes.Runes(data))
}

func decodeUTF16BE(data []byte) string {
	runes := make([]rune, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		runes = append(runes, rune(data[i])<<8|rune(data[i+1]))
	}
	return string(runes)
}

func decodeUTF16LE(data []byte) string {
	runes := make([]rune, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		runes = append(runes, rune(data[i+1])<<8|rune(data[i]))
	}
	return string(runes)
}

func extractDOCXText(raw []byte) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("open DOCX ZIP: %w", err)
	}

	var names []string
	files := map[string]*zip.File{}
	for _, f := range r.File {
		files[f.Name] = f
		if isDOCXTextPart(f.Name) {
			names = append(names, f.Name)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no WordprocessingML text parts found")
	}
	sort.Strings(names)

	acc := newExtractedTextAccumulator(maxExtractedTextBytes)
	for _, name := range names {
		text, err := readDOCXXMLText(files[name], maxExtractedTextBytes)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}
		if strings.TrimSpace(text) != "" {
			if err := acc.AddSection(text); err != nil {
				return "", err
			}
		}
	}
	return acc.String(), nil
}

func isDOCXTextPart(name string) bool {
	if !strings.HasPrefix(name, "word/") || !strings.HasSuffix(name, ".xml") {
		return false
	}
	base := filepath.Base(name)
	return base == "document.xml" ||
		strings.HasPrefix(base, "header") ||
		strings.HasPrefix(base, "footer") ||
		base == "footnotes.xml" ||
		base == "endnotes.xml" ||
		base == "comments.xml"
}

func readDOCXXMLText(f *zip.File, limit int64) (string, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return "", fmt.Errorf("document part exceeds extract limit")
	}
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	return collectWordprocessingText(io.LimitReader(rc, limit+1), limit)
}

func collectWordprocessingText(r io.Reader, limit int64) (string, error) {
	decoder := xml.NewDecoder(r)
	acc := newExtractedTextAccumulator(limit)
	inText := false
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" || t.Name.Local == "instrText" {
				inText = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" || t.Name.Local == "instrText" {
				inText = false
			}
			if t.Name.Local == "p" || t.Name.Local == "tr" {
				if err := acc.AddRaw("\n"); err != nil {
					return "", err
				}
			}
		case xml.CharData:
			if inText {
				if err := acc.AddRaw(string(t)); err != nil {
					return "", err
				}
			}
		}
	}
	return normalizeExtractedText(acc.String()), nil
}

func extractXLSXText(raw []byte) (string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(raw), excelize.Options{
		UnzipSizeLimit:    maxExtractedTextBytes,
		UnzipXMLSizeLimit: maxExtractedTextBytes,
	})
	if err != nil {
		return "", fmt.Errorf("open XLSX: %w", err)
	}
	defer func() { _ = f.Close() }()

	acc := newExtractedTextAccumulator(maxExtractedTextBytes)
	for _, sheetName := range f.GetSheetList() {
		rows, err := f.GetRows(sheetName, excelize.Options{RawCellValue: false})
		if err != nil {
			return "", fmt.Errorf("read sheet %s: %w", sheetName, err)
		}
		sheet := newExtractedTextAccumulator(maxExtractedTextBytes)
		for _, row := range rows {
			line := strings.TrimRight(strings.Join(row, "\t"), "\t")
			if strings.TrimSpace(line) != "" {
				if err := sheet.AddLine(line); err != nil {
					return "", err
				}
			}
		}
		if strings.TrimSpace(sheet.String()) != "" {
			if err := acc.AddSection(sheetName + "\n" + sheet.String()); err != nil {
				return "", err
			}
		}
	}
	return acc.String(), nil
}

func normalizeExtractedText(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

type extractedTextAccumulator struct {
	limit int64
	size  int64
	parts []string
}

func newExtractedTextAccumulator(limit int64) *extractedTextAccumulator {
	return &extractedTextAccumulator{limit: limit}
}

func (a *extractedTextAccumulator) AddLine(text string) error {
	return a.AddRaw(text + "\n")
}

func (a *extractedTextAccumulator) AddSection(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if len(a.parts) > 0 {
		if err := a.AddRaw("\n\n"); err != nil {
			return err
		}
	}
	return a.AddRaw(text)
}

func (a *extractedTextAccumulator) AddRaw(text string) error {
	if text == "" {
		return nil
	}
	a.size += int64(len(text))
	if a.size > a.limit {
		return fmt.Errorf("extracted text exceeds safe egress limit (%d bytes)", a.limit)
	}
	a.parts = append(a.parts, text)
	return nil
}

func (a *extractedTextAccumulator) String() string {
	return strings.Join(a.parts, "")
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, fmt.Errorf("extracted stream exceeds safe egress limit (%d bytes)", limit)
	}
	return buf.Bytes(), nil
}
