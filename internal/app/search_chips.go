package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"universal-search/internal/query"
	"universal-search/internal/store"
)

// toSearchResultDTO converts a store.SearchResult to a SearchResultDTO.
func toSearchResultDTO(r store.SearchResult) SearchResultDTO {
	return SearchResultDTO{
		FilePath:      r.File.Path,
		FileName:      filepath.Base(r.File.Path),
		FileType:      r.File.FileType,
		Extension:     r.File.Extension,
		SizeBytes:     r.File.SizeBytes,
		ThumbnailPath: r.File.ThumbnailPath,
		StartTime:     r.StartTime,
		EndTime:       r.EndTime,
		Score:         1 - r.Distance/2,
		ModifiedAt:    r.File.ModifiedAt.Unix(),
	}
}

// parseDenyList converts a slice of "field|op|value" strings into ClauseKey values.
func parseDenyList(denyList []string) []query.ClauseKey {
	if len(denyList) == 0 {
		return nil
	}
	keys := make([]query.ClauseKey, 0, len(denyList))
	for _, s := range denyList {
		parts := strings.SplitN(s, "|", 3)
		if len(parts) != 3 {
			continue
		}
		keys = append(keys, query.ClauseKey{
			Field: query.FieldEnum(parts[0]),
			Op:    query.Op(parts[1]),
			Value: parts[2],
		})
	}
	return keys
}

// buildChipDTOs converts a FilterSpec into a slice of ChipDTO values for the frontend.
func buildChipDTOs(spec query.FilterSpec) []ChipDTO {
	var chips []ChipDTO
	for _, c := range spec.Must {
		if chip, ok := clauseToChip(c, false, "must"); ok {
			chips = append(chips, chip)
		}
	}
	for _, c := range spec.MustNot {
		if chip, ok := clauseToChip(c, true, "must_not"); ok {
			chips = append(chips, chip)
		}
	}
	for _, c := range spec.Should {
		if chip, ok := clauseToChip(c, false, "should"); ok {
			chips = append(chips, chip)
		}
	}
	return chips
}

// clauseToChip converts a single Clause to a ChipDTO with a human-readable label.
func clauseToChip(c query.Clause, negate bool, clauseType string) (ChipDTO, bool) {
	var label, valueStr string

	switch c.Field {
	case query.FieldFileType:
		s, ok := c.Value.(string)
		if !ok {
			return ChipDTO{}, false
		}
		valueStr = s
		label = fileTypeLabel(s)

	case query.FieldExtension:
		switch v := c.Value.(type) {
		case string:
			valueStr = v
			label = v
		case []string:
			valueStr = strings.Join(v, ",")
			label = strings.Join(v, ", ")
		default:
			return ChipDTO{}, false
		}

	case query.FieldSizeBytes:
		var bytes int64
		switch v := c.Value.(type) {
		case int64:
			bytes = v
		case int:
			bytes = int64(v)
		default:
			return ChipDTO{}, false
		}
		valueStr = fmt.Sprintf("%d", bytes)
		opStr := opSymbol(c.Op)
		label = fmt.Sprintf("%s %s", opStr, formatBytes(bytes))

	case query.FieldModifiedAt:
		var t time.Time
		switch v := c.Value.(type) {
		case time.Time:
			t = v
		case int64:
			t = time.Unix(v, 0)
		case int:
			t = time.Unix(int64(v), 0)
		default:
			return ChipDTO{}, false
		}
		valueStr = fmt.Sprintf("%d", t.Unix())
		switch c.Op {
		case query.OpGte, query.OpGt:
			label = "Since " + t.Format("Jan 2")
		case query.OpLte, query.OpLt:
			label = "Before " + t.Format("Jan 2")
		default:
			label = t.Format("Jan 2")
		}

	case query.FieldPath:
		s, ok := c.Value.(string)
		if !ok {
			return ChipDTO{}, false
		}
		valueStr = s
		label = "Path: " + s

	default:
		s, ok := c.Value.(string)
		if !ok {
			return ChipDTO{}, false
		}
		valueStr = s
		label = s
	}

	if negate {
		label = "Not " + label
	}

	// Use fmt.Sprintf("%v", c.Value) to match merge.go's clauseValueString serialization
	// so that denylist lookups resolve correctly for all value types (e.g. []string in_set).
	clauseKey := fmt.Sprintf("%s|%s|%v", c.Field, c.Op, c.Value)

	return ChipDTO{
		Label:      label,
		Field:      string(c.Field),
		Op:         string(c.Op),
		Value:      valueStr,
		ClauseKey:  clauseKey,
		ClauseType: clauseType,
	}, true
}

// fileTypeLabel returns a human-readable label for a file_type value.
func fileTypeLabel(ft string) string {
	switch ft {
	case "image":
		return "Images"
	case "video":
		return "Videos"
	case "audio":
		return "Audio"
	case "document":
		return "Documents"
	case "text":
		return "Text"
	default:
		if ft == "" {
			return ""
		}
		return strings.ToUpper(ft[:1]) + ft[1:]
	}
}

// opSymbol returns a human-readable operator symbol.
func opSymbol(op query.Op) string {
	switch op {
	case query.OpGt:
		return ">"
	case query.OpGte:
		return ">="
	case query.OpLt:
		return "<"
	case query.OpLte:
		return "<="
	case query.OpEq:
		return "="
	case query.OpNeq:
		return "!="
	default:
		return string(op)
	}
}

// formatBytes formats a byte count as a human-readable size string.
func formatBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%d GB", b/GB)
	case b >= MB:
		return fmt.Sprintf("%d MB", b/MB)
	case b >= KB:
		return fmt.Sprintf("%d KB", b/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
