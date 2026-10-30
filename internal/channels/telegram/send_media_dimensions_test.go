package telegram

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func writePNGHeader(t *testing.T, width, height uint32) string {
	t.Helper()
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	_ = binary.Write(&ihdr, binary.BigEndian, width)
	_ = binary.Write(&ihdr, binary.BigEndian, height)
	ihdr.Write([]byte{8, 2, 0, 0, 0})

	var file bytes.Buffer
	file.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&file, binary.BigEndian, uint32(ihdr.Len()-4))
	file.Write(ihdr.Bytes())
	_ = binary.Write(&file, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))

	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, file.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClassifyTelegramOutboundMedia_PhotoDimensionLimits(t *testing.T) {
	cases := []struct {
		name          string
		width, height uint32
		want          telegramMediaKind
	}{
		{"desktop full page", 1440, 5237, telegramMediaPhoto},
		{"ratio exactly 20", 375, 7500, telegramMediaPhoto},
		{"mobile full page ratio above 20", 375, 7735, telegramMediaDocument},
		{"wide banner ratio above 20", 8000, 300, telegramMediaDocument},
		{"side sum above 10000", 5001, 5000, telegramMediaDocument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writePNGHeader(t, tc.width, tc.height)
			kind, groupable, _ := classifyTelegramOutboundMedia(bus.MediaAttachment{URL: path, ContentType: "image/png"}, nil)
			if kind != tc.want {
				t.Fatalf("kind = %q, want %q", kind, tc.want)
			}
			if !groupable {
				t.Fatal("image must stay groupable")
			}
		})
	}
}

func TestClassifyTelegramOutboundMedia_UndecodableImageStaysPhoto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.png")
	if err := os.WriteFile(path, []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	kind, _, _ := classifyTelegramOutboundMedia(bus.MediaAttachment{URL: path, ContentType: "image/png"}, nil)
	if kind != telegramMediaPhoto {
		t.Fatalf("kind = %q, want photo", kind)
	}
}

func TestPrepareTelegramMediaItems_KeepsContentWhenEveryAttachmentHasCaption(t *testing.T) {
	ch := &Channel{config: config.TelegramConfig{MediaMaxBytes: defaultMediaMaxBytes}}
	msg := bus.OutboundMessage{
		Content: "Final reply for the founder",
		Media: []bus.MediaAttachment{
			{URL: writePNGHeader(t, 1440, 3000), ContentType: "image/png", Caption: "Desktop"},
			{URL: writePNGHeader(t, 375, 3000), ContentType: "image/png", Caption: "Mobile"},
		},
	}

	items, err := ch.prepareTelegramMediaItems(msg)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].followUpText != "" {
		t.Fatalf("first item follow-up = %q, want empty", items[0].followUpText)
	}
	if !strings.Contains(items[1].followUpText, "Final reply for the founder") {
		t.Fatalf("reply text dropped, last follow-up = %q", items[1].followUpText)
	}
}

type mediaGroupFailingCaller struct {
	recordingTelegramCaller
}

func (c *mediaGroupFailingCaller) Call(ctx context.Context, url string, data *ta.RequestData) (*ta.Response, error) {
	if strings.HasSuffix(url, "/sendMediaGroup") {
		c.calls = append(c.calls, recordedTelegramCall{method: "sendMediaGroup", body: map[string]any{}})
		return &ta.Response{Ok: false, Error: &ta.Error{
			ErrorCode:   400,
			Description: `Bad Request: failed to send message #2 with the error message "PHOTO_INVALID_DIMENSIONS"`,
		}}, nil
	}
	return c.recordingTelegramCaller.Call(ctx, url, data)
}

func TestSendMediaMessage_DeliversTextWhenMediaGroupFails(t *testing.T) {
	caller := &mediaGroupFailingCaller{}
	bot, err := telego.NewBot(
		"123456:abcdefghijklmnopqrstuvwxyzABCDE1234",
		telego.WithAPICaller(caller),
		telego.WithDiscardLogger(),
	)
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}
	ch := &Channel{
		BaseChannel: channels.NewBaseChannel("telegram", nil, nil),
		bot:         bot,
		config:      config.TelegramConfig{MediaMaxBytes: defaultMediaMaxBytes},
	}
	msg := bus.OutboundMessage{
		Content: "Final reply for the founder",
		Media: []bus.MediaAttachment{
			{URL: writePNGHeader(t, 1440, 3000), ContentType: "image/png", Caption: "Desktop"},
			{URL: writePNGHeader(t, 375, 3000), ContentType: "image/png", Caption: "Mobile"},
		},
	}

	if err := ch.sendMediaMessage(context.Background(), 123, msg, 0, 0); err == nil {
		t.Fatal("expected the media group error to be returned")
	}

	var texts []string
	for _, call := range caller.calls {
		if call.method == "sendMessage" {
			raw, _ := json.Marshal(call.body["text"])
			texts = append(texts, string(raw))
		}
	}
	joined := strings.Join(texts, "\n")
	for _, want := range []string{"Desktop", "Mobile", "Final reply for the founder"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("text %q not delivered after media failure; sent: %v", want, texts)
		}
	}
}
