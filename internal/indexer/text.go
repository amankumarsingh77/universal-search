package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxTextBytes = 8000 // ~8K tokens worth of text, within Gemini's 8192 token context

// ExtractText reads text content from a file, truncating to maxTextBytes.
// For PDFs, returns empty string since PDFs use binary embedding via Gemini.
func ExtractText(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".pdf":
		return extractPDFText(path)
	default:
		return extractPlainText(path)
	}
}

func extractPlainText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	content := string(data)
	if len(content) > maxTextBytes {
		content = content[:maxTextBytes]
	}
	return content, nil
}

func extractPDFText(_ string) (string, error) {
	// For PDFs, we embed the raw bytes via Gemini's multimodal endpoint
	// rather than extracting text. This placeholder returns empty
	// to signal the pipeline should use EmbedBytes instead.
	return "", nil
}
