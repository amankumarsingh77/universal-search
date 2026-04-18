package chunker

import (
	"path/filepath"
	"strings"
)

type FileType string

const (
	TypeText     FileType = "text"
	TypeImage    FileType = "image"
	TypeVideo    FileType = "video"
	TypeAudio    FileType = "audio"
	TypeDocument FileType = "document"
	TypeUnknown  FileType = ""
)

var imageExtensions = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".heic": "image/heic",
	".heif": "image/heif",
	".gif":  "image/gif",
}

var videoExtensions = map[string]string{
	".mp4":  "video/mp4",
	".mpeg": "video/mpeg",
	".mpg":  "video/mpeg",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".flv":  "video/x-flv",
	".webm": "video/webm",
	".wmv":  "video/x-ms-wmv",
	".3gp":  "video/3gpp",
}

var audioExtensions = map[string]string{
	".wav":  "audio/wav",
	".mp3":  "audio/mpeg",
	".aiff": "audio/aiff",
	".aac":  "audio/aac",
	".ogg":  "audio/ogg",
	".flac": "audio/flac",
}

func Classify(filePath string) FileType {
	ext := strings.ToLower(filepath.Ext(filePath))

	if _, ok := imageExtensions[ext]; ok {
		return TypeImage
	}
	if _, ok := videoExtensions[ext]; ok {
		return TypeVideo
	}
	if _, ok := audioExtensions[ext]; ok {
		return TypeAudio
	}
	if IsDocumentFile(ext) {
		return TypeDocument
	}

	isText, err := IsTextFile(filePath)
	if err == nil && isText {
		return TypeText
	}

	return TypeUnknown
}

func MimeType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	if mime, ok := imageExtensions[ext]; ok {
		return mime
	}
	if mime, ok := videoExtensions[ext]; ok {
		return mime
	}
	if mime, ok := audioExtensions[ext]; ok {
		return mime
	}
	if ext == ".pdf" {
		return "application/pdf"
	}
	return ""
}

func ChunkFile(filePath string) ([]Chunk, FileType, error) {
	ft := Classify(filePath)

	switch ft {
	case TypeText:
		chunks, err := ChunkText(filePath)
		return chunks, ft, err
	case TypeImage:
		mime := MimeType(filePath)
		chunks, err := ChunkBinary(filePath, mime)
		return chunks, ft, err
	case TypeVideo:
		chunks, err := ChunkVideo(filePath)
		return chunks, ft, err
	case TypeAudio:
		mime := MimeType(filePath)
		chunks, err := ChunkBinary(filePath, mime)
		return chunks, ft, err
	case TypeDocument:
		chunks, err := ChunkDocument(filePath)
		return chunks, ft, err
	default:
		return nil, TypeUnknown, nil
	}
}
