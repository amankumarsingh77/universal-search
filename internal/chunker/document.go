package chunker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"findo/internal/apperr"
)

const (
	pdfPagesPerChunk = 6
	pdfPageOverlap   = 1
	officeTimeout    = 30 * time.Second
)

var modernOfficeExt = map[string]bool{
	".docx": true, ".pptx": true, ".xlsx": true,
}

var legacyOfficeExt = map[string]bool{
	".doc": true, ".ppt": true, ".xls": true,
	".odt": true, ".odp": true, ".ods": true,
	".rtf": true,
}

// HasLibreOffice reports whether the libreoffice binary is available on PATH.
func HasLibreOffice() bool {
	_, err := exec.LookPath("libreoffice")
	return err == nil
}

func hasQPDF() bool {
	_, err := exec.LookPath("qpdf")
	return err == nil
}

// IsDocumentFile reports whether the extension is a supported document format.
func IsDocumentFile(ext string) bool {
	ext = strings.ToLower(ext)
	if ext == ".pdf" || modernOfficeExt[ext] {
		return true
	}
	return legacyOfficeExt[ext]
}

// IsModernOffice reports whether the extension is an OOXML/ODF file (.docx, .pptx, .xlsx, …).
func IsModernOffice(ext string) bool {
	return modernOfficeExt[strings.ToLower(ext)]
}

// IsLegacyOffice reports whether the extension is a legacy office format (.doc, .ppt, .xls, …).
func IsLegacyOffice(ext string) bool {
	return legacyOfficeExt[strings.ToLower(ext)]
}

// ChunkDocument produces text chunks from a document file (PDF, office, RTF, ODF).
func ChunkDocument(filePath string) ([]Chunk, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	if ext == ".pdf" {
		return chunkPDF(filePath)
	}

	if IsModernOffice(ext) {
		return chunkModernOffice(filePath, ext)
	}

	if IsLegacyOffice(ext) {
		return chunkLegacyOffice(filePath)
	}

	return nil, apperr.Wrap(apperr.ErrUnsupportedFormat.Code, "unsupported document format: "+ext, nil)
}

func chunkModernOffice(filePath, ext string) ([]Chunk, error) {
	var chunks []Chunk

	if HasLibreOffice() {
		pdfChunks, err := convertAndChunkPDF(filePath)
		if err == nil {
			chunks = append(chunks, pdfChunks...)
		}
	}

	text, err := ExtractOfficeText(filePath)
	if err != nil {
		if len(chunks) > 0 {
			return chunks, nil
		}
		return nil, apperr.Wrap(apperr.ErrExtractionFailed.Code, "extract text from "+ext+" failed", err)
	}

	if text != "" {
		textChunks, _ := chunkTextContent(text, len(chunks))
		chunks = append(chunks, textChunks...)
	}

	if len(chunks) == 0 {
		return nil, apperr.Wrap(apperr.ErrExtractionFailed.Code, "no content extracted from "+filepath.Base(filePath), nil)
	}

	return chunks, nil
}

func chunkLegacyOffice(filePath string) ([]Chunk, error) {
	if !HasLibreOffice() {
		return nil, apperr.Wrap(apperr.ErrExtractionFailed.Code,
			"libreoffice required for "+filepath.Ext(filePath)+" — legacy Office formats need LibreOffice for conversion", nil)
	}

	chunks, err := convertAndChunkPDF(filePath)
	if err != nil {
		return nil, apperr.Wrap(apperr.ErrExtractionFailed.Code, "legacy office conversion failed", err)
	}
	return chunks, nil
}

func convertAndChunkPDF(filePath string) ([]Chunk, error) {
	tmpDir, err := os.MkdirTemp("", "docconvert-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	pdfPath, err := convertToPDF(filePath, tmpDir)
	if err != nil {
		return nil, err
	}

	return chunkPDF(pdfPath)
}

func convertToPDF(inputPath, outputDir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), officeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "libreoffice",
		"--headless",
		"--convert-to", "pdf",
		"--outdir", outputDir,
		inputPath,
	)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("libreoffice convert: %w", err)
	}

	base := filepath.Base(inputPath)
	ext := filepath.Ext(base)
	pdfName := base[:len(base)-len(ext)] + ".pdf"
	pdfPath := filepath.Join(outputDir, pdfName)

	if _, err := os.Stat(pdfPath); err != nil {
		return "", fmt.Errorf("converted PDF not found: %w", err)
	}
	return pdfPath, nil
}

func chunkPDF(pdfPath string) ([]Chunk, error) {
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, err
	}

	pageCount := countPDFPages(data)
	var chunks []Chunk

	if pageCount <= pdfPagesPerChunk || !hasQPDF() {
		chunks = append(chunks, Chunk{
			Content:   data,
			MimeType:  "application/pdf",
			PageStart: 1,
			PageEnd:   pageCount,
			Index:     0,
		})
	} else {
		tmpDir, err := os.MkdirTemp("", "pdfsplit-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmpDir)

		idx := 0
		step := pdfPagesPerChunk - pdfPageOverlap
		for start := 1; start <= pageCount; start += step {
			end := start + pdfPagesPerChunk - 1
			if end > pageCount {
				end = pageCount
			}

			splitPath, err := splitPDFPages(pdfPath, tmpDir, start, end)
			if err != nil {
				continue
			}
			splitData, err := os.ReadFile(splitPath)
			if err != nil {
				continue
			}

			chunks = append(chunks, Chunk{
				Content:   splitData,
				MimeType:  "application/pdf",
				PageStart: start,
				PageEnd:   end,
				Index:     idx,
			})
			idx++

			if end >= pageCount {
				break
			}
		}
	}

	fullText := extractPDFText(data)
	if fullText != "" {
		textChunks, _ := chunkTextContent(fullText, len(chunks))
		chunks = append(chunks, textChunks...)
	}

	return chunks, nil
}

var pageCountRegex = regexp.MustCompile(`/Type\s*/Page[^s]`)

func countPDFPages(data []byte) int {
	matches := pageCountRegex.FindAll(data, -1)
	if len(matches) == 0 {
		return 1
	}
	return len(matches)
}

func splitPDFPages(pdfPath, outputDir string, startPage, endPage int) (string, error) {
	outPath := filepath.Join(outputDir, fmt.Sprintf("pages_%d_%d.pdf", startPage, endPage))
	pageRange := fmt.Sprintf("%d-%d", startPage, endPage)

	err := exec.Command("qpdf", pdfPath,
		"--pages", pdfPath, pageRange, "--",
		outPath,
	).Run()
	if err != nil {
		return "", fmt.Errorf("qpdf split: %w", err)
	}
	return outPath, nil
}

func extractPDFText(data []byte) string {
	var text bytes.Buffer
	for _, line := range bytes.Split(data, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("stream")) || bytes.HasPrefix(line, []byte("endstream")) {
			continue
		}
		cleaned := stripPDFBinary(line)
		if len(cleaned) > 0 {
			text.Write(cleaned)
			text.WriteByte('\n')
		}
	}
	result := text.String()
	if len(result) > maxTextBytes {
		result = result[:maxTextBytes]
	}
	return result
}

func stripPDFBinary(line []byte) []byte {
	start := bytes.Index(line, []byte("("))
	end := bytes.LastIndex(line, []byte(")"))
	if start >= 0 && end > start {
		return line[start+1 : end]
	}

	start = bytes.Index(line, []byte("<"))
	end = bytes.LastIndex(line, []byte(">"))
	if start >= 0 && end > start {
		hexBytes := line[start+1 : end]
		decoded := decodeHexString(hexBytes)
		if len(decoded) > 0 {
			return decoded
		}
	}
	return nil
}

func decodeHexString(hexBytes []byte) []byte {
	var result []byte
	for i := 0; i+1 < len(hexBytes); i += 2 {
		high, ok1 := hexVal(hexBytes[i])
		low, ok2 := hexVal(hexBytes[i+1])
		if !ok1 || !ok2 {
			return nil
		}
		b := high<<4 | low
		if b >= 32 && b < 127 {
			result = append(result, b)
		}
	}
	return result
}

func hexVal(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

func chunkTextContent(text string, startIndex int) ([]Chunk, error) {
	chunks := chunkString(strings.TrimSpace(text), startIndex)
	if len(chunks) == 0 {
		return nil, nil
	}

	totalLen := 0
	for _, c := range chunks {
		totalLen += len(c.Text)
	}
	if totalLen < 100 {
		return nil, nil
	}

	return chunks, nil
}
