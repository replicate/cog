package mime

import (
	"mime"
	"strings"
)

var typeToExtension = map[string]string{
	"application/epub+zip":                            ".epub",
	"application/gzip":                                ".gz",
	"application/java-archive":                        ".jar",
	"application/json":                                ".json",
	"application/jsonl":                               ".jsonl",
	"application/ld+json":                             ".jsonld",
	"application/msword":                              ".doc",
	"application/octet-stream":                        ".bin",
	"application/ogg":                                 ".ogx",
	"application/pdf":                                 ".pdf",
	"application/rtf":                                 ".rtf",
	"application/vnd.amazon.ebook":                    ".azw",
	"application/vnd.apple.installer+xml":             ".mpkg",
	"application/vnd.ms-excel":                        ".xls",
	"application/vnd.ms-fontobject":                   ".eot",
	"application/vnd.ms-powerpoint":                   ".ppt",
	"application/vnd.oasis.opendocument.presentation": ".odp",
	"application/vnd.oasis.opendocument.spreadsheet":  ".ods",
	"application/vnd.oasis.opendocument.text":         ".odt",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   ".docx",
	"application/vnd.rar":           ".rar",
	"application/vnd.visio":         ".vsd",
	"application/x-7z-compressed":   ".7z",
	"application/x-abiword":         ".abw",
	"application/x-bzip":            ".bz",
	"application/x-bzip2":           ".bz2",
	"application/x-cdf":             ".cda",
	"application/x-csh":             ".csh",
	"application/x-freearc":         ".arc",
	"application/x-httpd-php":       ".php",
	"application/x-ndjson":          ".ndjson",
	"application/x-sh":              ".sh",
	"application/x-shockwave-flash": ".swf",
	"application/x-tar":             ".tar",
	"application/xhtml+xml":         ".xhtml",
	"application/xml":               ".xml",
	"application/zip":               ".zip",

	"audio/aac":               ".aac",
	"audio/midi audio/x-midi": ".midi",
	"audio/mpeg":              ".mp3",
	"audio/ogg":               ".oga",
	"audio/opus":              ".opus",
	"audio/wav":               ".wav",
	"audio/webm":              ".weba",

	"font/otf":   ".otf",
	"font/ttf":   ".ttf",
	"font/woff":  ".woff",
	"font/woff2": ".woff2",

	"image/bmp":                ".bmp",
	"image/x-ms-bmp":           ".bmp",
	"image/gif":                ".gif",
	"image/jpeg":               ".jpg",
	"image/png":                ".png",
	"image/svg+xml":            ".svg",
	"image/tiff":               ".tiff",
	"image/vnd.microsoft.icon": ".ico",
	"image/webp":               ".webp",

	"model/gltf-binary": ".glb",
	"model/mtl":         ".mtl",
	"model/obj":         ".obj",

	"text/calendar":   ".ics",
	"text/css":        ".css",
	"text/csv":        ".csv",
	"text/html":       ".html",
	"text/javascript": ".js",
	"text/markdown":   ".md",
	"text/plain":      ".txt",

	"video/3gpp":      ".3gp",
	"video/3gpp2":     ".3gp2",
	"video/mp2t":      ".ts",
	"video/mp4":       ".mp4",
	"video/mpeg":      ".mpeg",
	"video/ogg":       ".ogv",
	"video/webm":      ".webm",
	"video/x-msvideo": ".avi",
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
