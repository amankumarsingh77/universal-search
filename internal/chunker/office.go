package chunker

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ExtractDocxText returns the plain text content of a .docx file.
func ExtractDocxText(filePath string) (string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()
			return parseWordXML(rc)
		}
	}
	return "", fmt.Errorf("word/document.xml not found in docx")
}

func parseWordXML(r io.Reader) (string, error) {
	decoder := xml.NewDecoder(r)
	var text strings.Builder
	inParagraph := false

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return text.String(), nil
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				if inParagraph {
					text.WriteByte('\n')
				}
				inParagraph = true
			case "tab":
				text.WriteByte('\t')
			case "br":
				text.WriteByte('\n')
			}
		case xml.EndElement:
			if t.Name.Local == "p" {
				text.WriteByte('\n')
				inParagraph = false
			}
		case xml.CharData:
			text.Write(t)
		}
	}

	return strings.TrimSpace(text.String()), nil
}

// ExtractPptxText returns the concatenated slide text from a .pptx file.
func ExtractPptxText(filePath string) (string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("open pptx: %w", err)
	}
	defer r.Close()

	var slideFiles []*zip.File
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slideFiles = append(slideFiles, f)
		}
	}

	sort.Slice(slideFiles, func(i, j int) bool {
		return extractSlideNumber(slideFiles[i].Name) < extractSlideNumber(slideFiles[j].Name)
	})

	var text strings.Builder
	for i, f := range slideFiles {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		slideText, err := parseSlideXML(rc)
		rc.Close()
		if err != nil {
			continue
		}
		slideText = strings.TrimSpace(slideText)
		if slideText != "" {
			if i > 0 {
				text.WriteString("\n\n")
			}
			text.WriteString(slideText)
		}
	}

	return strings.TrimSpace(text.String()), nil
}

func extractSlideNumber(name string) int {
	base := filepath.Base(name)
	num := strings.TrimPrefix(base, "slide")
	num = strings.TrimSuffix(num, ".xml")
	n := 0
	for _, c := range num {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func parseSlideXML(r io.Reader) (string, error) {
	decoder := xml.NewDecoder(r)
	var text strings.Builder
	var inText bool

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return text.String(), nil
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inText = true
			}
			if t.Name.Local == "p" && text.Len() > 0 {
				text.WriteByte('\n')
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
			}
		case xml.CharData:
			if inText {
				text.Write(t)
			}
		}
	}

	return text.String(), nil
}

// ExtractXlsxText returns the flattened cell text across every sheet of an .xlsx file.
func ExtractXlsxText(filePath string) (string, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return "", fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	var text strings.Builder
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			continue
		}
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(sheet)
		text.WriteByte('\n')
		for _, row := range rows {
			text.WriteString(strings.Join(row, "\t"))
			text.WriteByte('\n')
		}
	}

	result := text.String()
	if len(result) > maxTextBytes {
		result = result[:maxTextBytes]
	}
	return strings.TrimSpace(result), nil
}

// ExtractOfficeText dispatches to the correct office text extractor based on file extension.
func ExtractOfficeText(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".docx":
		return ExtractDocxText(filePath)
	case ".pptx":
		return ExtractPptxText(filePath)
	case ".xlsx":
		return ExtractXlsxText(filePath)
	default:
		return "", fmt.Errorf("unsupported office format: %s", ext)
	}
}
