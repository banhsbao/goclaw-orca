package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func TestConn_ReadLoop_StringIDRequestEchoesSameID(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()

	handler := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return map[string]string{"method": method}, nil
	}
	conn := NewConn(clientW, clientR, handler, nil)
	conn.Start()

	go serverW.Write([]byte(`{"jsonrpc":"2.0","id":"1","method":"session/request_permission","params":{}}` + "\n"))

	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(serverR)
		if sc.Scan() {
			lines <- sc.Text()
		}
	}()

	select {
	case line := <-lines:
		var resp struct {
			ID     json.RawMessage   `json:"id"`
			Result map[string]string `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("bad response %q: %v", line, err)
		}
		if string(resp.ID) != `"1"` {
			t.Fatalf("expected id \"1\", got %s", resp.ID)
		}
		if resp.Result["method"] != "session/request_permission" {
			t.Fatalf("unexpected result %v", resp.Result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no response for string-id request")
	}
}

func TestConn_Call_AcceptsStringIDResponse(t *testing.T) {
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	conn := NewConn(clientW, clientR, nil, nil)
	conn.Start()

	go func() {
		sc := bufio.NewScanner(serverR)
		if !sc.Scan() {
			return
		}
		var req jsonrpcMessage
		_ = json.Unmarshal(sc.Bytes(), &req)
		serverW.Write([]byte(`{"jsonrpc":"2.0","id":"` + itoa(*req.ID) + `","result":{"ok":true}}` + "\n"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var res struct{ OK bool }
	if err := conn.Call(ctx, "ping", nil, &res); err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if !res.OK {
		t.Fatal("expected ok result")
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
