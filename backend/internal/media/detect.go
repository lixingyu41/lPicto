package media

import (
	"mime"
	"path/filepath"
	"strings"
)

type Detection struct {
	MediaType string
	MimeType  string
	Ext       string
	OK        bool
}

var imageExts = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
	".tif":  "image/tiff",
	".tiff": "image/tiff",
	".heic": "image/heic",
	".heif": "image/heif",
}

var videoExts = map[string]string{
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".mkv":  "video/x-matroska",
	".avi":  "video/x-msvideo",
	".m4v":  "video/x-m4v",
}

var audioExts = map[string]string{
	".mp3":  "audio/mpeg",
	".aac":  "audio/aac",
	".m4a":  "audio/mp4",
	".flac": "audio/flac",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".opus": "audio/ogg",
	".wma":  "audio/x-ms-wma",
	".ape":  "audio/x-ape",
	".alac": "audio/mp4",
	".aif":  "audio/aiff",
	".aiff": "audio/aiff",
	".amr":  "audio/amr",
	".ac3":  "audio/ac3",
	".mka":  "audio/x-matroska",
	".dsf":  "audio/x-dsf",
	".dff":  "audio/x-dff",
}

func DetectByPath(filename string) Detection {
	ext := strings.ToLower(filepath.Ext(filename))
	if mt, ok := imageExts[ext]; ok {
		return Detection{MediaType: "image", MimeType: mt, Ext: strings.TrimPrefix(ext, "."), OK: true}
	}
	if mt, ok := videoExts[ext]; ok {
		return Detection{MediaType: "video", MimeType: mt, Ext: strings.TrimPrefix(ext, "."), OK: true}
	}
	if mt, ok := audioExts[ext]; ok {
		return Detection{MediaType: "audio", MimeType: mt, Ext: strings.TrimPrefix(ext, "."), OK: true}
	}
	if mt := mime.TypeByExtension(ext); mt != "" {
		return Detection{MimeType: mt, Ext: strings.TrimPrefix(ext, ".")}
	}
	return Detection{Ext: strings.TrimPrefix(ext, ".")}
}

func BrowserAudioPlayable(ext, codec string) bool {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	codec = strings.ToLower(strings.TrimSpace(codec))
	switch ext {
	case "mp3":
		return codec == "" || codec == "mp3"
	case "mp4", "m4a", "m4v", "aac":
		return codec == "" || codec == "aac"
	case "flac":
		return codec == "" || codec == "flac"
	case "wav":
		return codec == "" || strings.HasPrefix(codec, "pcm_")
	case "ogg", "oga":
		return codec == "" || codec == "vorbis" || codec == "opus" || codec == "flac"
	case "opus":
		return codec == "" || codec == "opus"
	default:
		return false
	}
}

func AudioMimeType(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "mp4", "m4a", "m4v", "aac", "alac":
		return "audio/mp4"
	case "mov":
		return "audio/quicktime"
	default:
		if detected := DetectByPath("audio." + strings.TrimPrefix(ext, ".")); detected.MimeType != "" {
			return detected.MimeType
		}
		return "audio/*"
	}
}

func BrowserImageDisplayable(mimeType string) bool {
	switch mimeType {
	case "image/jpeg", "image/png", "image/webp", "image/gif", "image/bmp":
		return true
	default:
		return false
	}
}
