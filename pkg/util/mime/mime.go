package mime

import (
	"mime"
	"strings"
)

var typeToExtension = map[string]string{
	"audio/aac":                    ".aac",
	"application/x-abiword":        ".abw",
	"application/x-freearc":        ".arc",
	"video/x-msvideo":              ".avi",
	"application/vnd.amazon.ebook": ".azw",
	"application/octet-stream":     ".bin",
	"image/bmp":                    ".bmp",
	"application/x-bzip":           ".bz",
	"application/x-bzip2":          ".bz2",
	"application/x-csh":            ".csh",
	"text/css":                     ".css",
	"text/csv":                     ".csv",
	"application/msword":           ".doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
	"application/vnd.ms-fontobject":                                           ".eot",
	"application/epub+zip":                                                    ".epub",
	"application/gzip":                                                        ".gz",
	"image/gif":                                                               ".gif",
	"text/html":                                                               ".html",
	"image/vnd.microsoft.icon":                                                ".ico",
	"text/calendar":                                                           ".ics",
	"application/java-archive":                                                ".jar",
	"image/jpeg":                                                              ".jpg",
	"text/javascript":                                                         ".js",
	"application/json":                                                        ".json",
	"application/ld+json":                                                     ".jsonld",
	"audio/midi audio/x-midi":                                                 ".midi",
	"audio/mpeg":                                                              ".mp3",
	"application/x-cdf":                                                       ".cda",
	"video/mp4":                                                               ".mp4",
	"video/mpeg":                                                              ".mpeg",
	"application/vnd.apple.installer+xml":                                     ".mpkg",
	"application/vnd.oasis.opendocument.presentation": ".odp",
	"application/vnd.oasis.opendocument.spreadsheet":  ".ods",
	"application/vnd.oasis.opendocument.text":         ".odt",
	"audio/ogg":                     ".oga",
	"video/ogg":                     ".ogv",
	"application/ogg":               ".ogx",
	"audio/opus":                    ".opus",
	"font/otf":                      ".otf",
	"image/png":                     ".png",
	"application/pdf":               ".pdf",
	"application/x-httpd-php":       ".php",
	"application/vnd.ms-powerpoint": ".ppt",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
	"application/vnd.rar":           ".rar",
	"application/rtf":               ".rtf",
	"application/x-sh":              ".sh",
	"image/svg+xml":                 ".svg",
	"application/x-shockwave-flash": ".swf",
	"application/x-tar":             ".tar",
	"image/tiff":                    ".tiff",
	"video/mp2t":                    ".ts",
	"font/ttf":                      ".ttf",
	"text/plain":                    ".txt",
	"application/vnd.visio":         ".vsd",
	"audio/wav":                     ".wav",
	"audio/webm":                    ".weba",
	"video/webm":                    ".webm",
	"image/webp":                    ".webp",
	"font/woff":                     ".woff",
	"font/woff2":                    ".woff2",
	"application/xhtml+xml":         ".xhtml",
	"application/vnd.ms-excel":      ".xls",
	"application/xml":               ".xml",
	"application/zip":               ".zip",
	"video/3gpp":                    ".3gp",
	"video/3gpp2":                   ".3gp2",
	"application/x-7z-compressed":   ".7z",
}

var extensionToType = map[string]string{}

func init() {
	for typ, ext := range typeToExtension {
		extensionToType[ext] = typ
	}
}

// ExtensionByType returns the file extension associated with the media type typ.
// When typ has no associated extension, ExtensionByType returns an empty string.
func ExtensionByType(typ string) string {
	// Lookup extension from pre-defined map
	ext := typeToExtension[typ]

	// Fall back to mime.ExtensionsByType
	if ext == "" {
		extensions, _ := mime.ExtensionsByType(typ)
		if len(extensions) > 0 {
			ext = extensions[0]
		}
	}

	return ext
}

// TypeByExtension returns the media type associated with the file extension ext.
// The extension ext should begin with a leading dot, as in ".json"
// When ext has no associated type, TypeByExtension returns "application/octet-stream"
func TypeByExtension(ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	// Lookup type from pre-defined map
	typ := extensionToType[ext]

	// Fall back to mime.TypeByExtension
	if typ == "" {
		typ = mime.TypeByExtension(ext)
	}

	// Default to "application/octet-stream"
	if typ == "" {
		typ = "application/octet-stream"
	}

	return typ
}
