package chunker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"findo/internal/apperr"
)

// FileType names the coarse content category of an indexed file.
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

// heicTranscoder converts a HEIC/HEIF file to JPEG bytes. The default
// implementation shells out to ffmpeg; tests inject a fake at the Registry
// boundary instead of mutating a package-level var.
type heicTranscoder func(filePath string) ([]byte, error)

// Registry holds chunker dependencies that benefit from injection (e.g. the
// HEIC transcoder, which normally shells out to ffmpeg).
type Registry struct {
	transcodeHEIC heicTranscoder
}

// NewRegistry constructs a Registry with the default ffmpeg-backed HEIC
// transcoder.
func NewRegistry() *Registry {
	return &Registry{transcodeHEIC: transcodeHEICToJPEG}
}

// WithHEICTranscoder returns a copy of r with the given HEIC transcoder
// function. Used in tests to avoid spawning ffmpeg.
func (r *Registry) WithHEICTranscoder(fn heicTranscoder) *Registry {
	cp := *r
	cp.transcodeHEIC = fn
	return &cp
}

// defaultRegistry is used by the package-level ChunkFile helper. Tests that
// need custom behavior should construct their own Registry.
var defaultRegistry = NewRegistry()

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

// Classify returns the FileType for a given file path based on its extension.
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

// MimeType returns the content MIME type inferred from a file's extension.
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

// ChunkFile dispatches to the appropriate chunker using the default registry.
func ChunkFile(filePath string) ([]Chunk, FileType, error) {
	return defaultRegistry.ChunkFile(filePath)
}

// ChunkFile dispatches to the appropriate chunker for the given file using
// this registry's configured dependencies.
func (r *Registry) ChunkFile(filePath string) ([]Chunk, FileType, error) {
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
				return nil, ft, apperr.Wrap(apperr.ErrFileUnreadable.Code, "cannot stat HEIC file", err)
			}
			if info.Size() > maxBinarySize {
				return nil, ft, apperr.Wrap(apperr.ErrFileTooLarge.Code,
					fmt.Sprintf("file too large (%d bytes, max %d)", info.Size(), maxBinarySize), nil)
			}

			data, err := r.transcodeHEIC(filePath)
			if err != nil {
				return nil, ft, apperr.Wrap(apperr.ErrExtractionFailed.Code, "HEIC transcoding failed", err)
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
