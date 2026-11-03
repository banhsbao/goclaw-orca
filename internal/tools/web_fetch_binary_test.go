package tools

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsBinaryContentType(t *testing.T) {
	tests := []struct {
		contentType string
		want        bool
	}{
		{"image/png", true},
		{"image/jpeg; charset=binary", true},
		{"application/pdf", true},
		{"application/octet-stream", true},
		{"video/mp4", true},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", true},
		{"image/svg+xml", false},
		{"text/html; charset=utf-8", false},
		{"application/json", false},
		{"text/plain", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isBinaryContentType(tt.contentType); got != tt.want {
			t.Errorf("isBinaryContentType(%q) = %v, want %v", tt.contentType, got, tt.want)
		}
	}
}

func TestLooksBinary(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{0xff, 0xd8, 0x9a}, 200)...)
	latin1 := []byte("Caf\xe9 cr\xe8me br\xfbl\xe9e is a dessert served in many restaurants around the world.")
	tests := []struct {
		name string
		body []byte
		want bool
	}{
		{"empty", nil, false},
		{"png with nul", png, true},
		{"high-byte noise", bytes.Repeat([]byte{0xff, 0xfe, 0x81}, 100), true},
		{"utf8 text", []byte("Xin chào, đây là trang tiếng Việt."), false},
		{"latin1 text", latin1, false},
	}
	for _, tt := range tests {
		if got := looksBinary(tt.body); got != tt.want {
			t.Errorf("%s: looksBinary = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFetchRawContent_BinaryResponses(t *testing.T) {
	pngBody := append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{0x00, 0xff}, 30000)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBody)
		case "/unlabeled":
			w.Header().Set("Content-Type", "")
			_, _ = w.Write(pngBody)
		case "/text":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(strings.Repeat("a", binarySniffBytes) + "hello\x00world"))
		}
	}))
	defer srv.Close()

	tool := NewWebFetchTool(WebFetchConfig{})
	for _, path := range []string{"/image.png", "/unlabeled"} {
		res, err := tool.fetchRawContent(context.Background(), srv.URL+path, "markdown", defaultFetchMaxChars, webFetchPolicy{})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", path, err)
		}
		if res.extractor != "binary" {
			t.Errorf("%s: extractor = %q, want binary", path, res.extractor)
		}
		if strings.ContainsRune(res.content, 0) || len(res.content) > 300 {
			t.Errorf("%s: content not replaced by notice: len=%d", path, len(res.content))
		}
		if !strings.Contains(res.content, "Binary content not shown") {
			t.Errorf("%s: missing notice, got %q", path, res.content)
		}
	}

	res, err := tool.fetchRawContent(context.Background(), srv.URL+"/text", "markdown", defaultFetchMaxChars, webFetchPolicy{})
	if err != nil {
		t.Fatalf("text: unexpected error: %v", err)
	}
	if res.content != strings.Repeat("a", binarySniffBytes)+"helloworld" {
		t.Errorf("text: content = %q, want NUL stripped", res.content)
	}
}
