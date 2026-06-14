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

func DetectByPath(filename string) Detection {
	ext := strings.ToLower(filepath.Ext(filename))
	if mt, ok := imageExts[ext]; ok {
		return Detection{MediaType: "image", MimeType: mt, Ext: strings.TrimPrefix(ext, "."), OK: true}
	}
	if mt, ok := videoExts[ext]; ok {
		return Detection{MediaType: "video", MimeType: mt, Ext: strings.TrimPrefix(ext, "."), OK: true}
	}
	if mt := mime.TypeByExtension(ext); mt != "" {
		return Detection{MimeType: mt, Ext: strings.TrimPrefix(ext, ".")}
	}
	return Detection{Ext: strings.TrimPrefix(ext, ".")}
}

func BrowserImageDisplayable(mimeType string) bool {
	switch mimeType {
	case "image/jpeg", "image/png", "image/webp", "image/gif", "image/bmp":
		return true
	default:
		return false
	}
}
