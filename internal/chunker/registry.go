package chunker

import (
	"fmt"
	"os"
	"os/exec"
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

// transcodeHEIC converts a HEIC/HEIF file to JPEG bytes using ffmpeg.
// It is a package-level var so tests can stub it without spawning a real process.
var transcodeHEIC = transcodeHEICToJPEG

// transcodeHEICToJPEG uses ffmpeg to decode a HEIC/HEIF image and return JPEG
// bytes via stdout. ffmpeg is already a project dependency (used for video
// thumbnails), so this introduces no new system requirements.
func transcodeHEICToJPEG(filePath string) ([]byte, error) {
	out, err := exec.Command("ffmpeg",
		"-i", filePath,
		"-f", "mjpeg",
		"-q:v", "4",
		"-frames:v", "1",
		"pipe:1",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("transcode HEIC to JPEG: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("transcode HEIC to JPEG: ffmpeg produced no output")
	}
	return out, nil
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
		ext := strings.ToLower(filepath.Ext(filePath))
		if ext == ".heic" || ext == ".heif" {
			info, err := os.Stat(filePath)
			if err != nil {
				return nil, ft, fmt.Errorf("stat binary file: %w", err)
			}
			if info.Size() > maxBinarySize {
				return nil, ft, fmt.Errorf("file too large (%d bytes, max %d): %s", info.Size(), maxBinarySize, filePath)
			}

			data, err := transcodeHEIC(filePath)
			if err != nil {
				return nil, ft, err
			}
			return []Chunk{{Content: data, MimeType: "image/jpeg", Index: 0}}, ft, nil
		}
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
