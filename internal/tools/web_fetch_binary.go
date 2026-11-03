package tools

import (
	"bytes"
	"fmt"
	"mime"
	"strings"
	"unicode/utf8"
)

const binarySniffBytes = 8192

var binaryMediaTypePrefixes = []string{
	"image/",
	"audio/",
	"video/",
	"font/",
}

var binaryMediaTypes = map[string]bool{
	"application/octet-stream":     true,
	"application/pdf":              true,
	"application/zip":              true,
	"application/gzip":             true,
	"application/x-gzip":           true,
	"application/x-tar":            true,
	"application/x-7z-compressed":  true,
	"application/x-rar-compressed": true,
	"application/vnd.rar":          true,
	"application/wasm":             true,
	"application/msword":           true,
	"application/vnd.ms-excel":     true,
}

func isBinaryContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "" || mediaType == "image/svg+xml" {
		return false
	}
	for _, prefix := range binaryMediaTypePrefixes {
		if strings.HasPrefix(mediaType, prefix) {
			return true
		}
	}
	if strings.HasPrefix(mediaType, "application/vnd.openxmlformats-officedocument.") {
		return true
	}
	return binaryMediaTypes[mediaType]
}

func looksBinary(body []byte) bool {
	sample := body
	if len(sample) > binarySniffBytes {
		sample = sample[:binarySniffBytes]
	}
	if len(sample) == 0 {
		return false
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return true
	}
	invalid := 0
	for rest := sample; len(rest) > 0; {
		r, size := utf8.DecodeRune(rest)
		if r == utf8.RuneError && size == 1 {
			invalid++
		}
		rest = rest[size:]
	}
	return invalid*10 > len(sample)
}

func binaryContentNotice(contentType string, size int64) string {
	if strings.TrimSpace(contentType) == "" {
		contentType = "unknown"
	}
	sizeText := "unknown size"
	if size >= 0 {
		sizeText = fmt.Sprintf("%d bytes", size)
	}
	return fmt.Sprintf("[Binary content not shown: %s, %s. web_fetch only returns readable text; this URL is a file, not a page. Do not fetch it again.]", contentType, sizeText)
}
