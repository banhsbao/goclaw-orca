package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestACPProcess_SetModel_SendsRequest(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	defer serverW.Close()
	defer serverR.Close()

	got := make(chan jsonrpcMessage, 1)
	go func() {
		buf := make([]byte, 32*1024)
		n, err := serverR.Read(buf)
		if err != nil {
			return
		}
		var req jsonrpcMessage
		if json.Unmarshal(buf[:n], &req) != nil {
			return
		}
		got <- req
		data, _ := json.Marshal(jsonrpcMessage{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{}`)})
		serverW.Write(append(data, '\n'))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := proc.SetModel(ctx, "sess-1", "gemini-4-argon-low"); err != nil {
		t.Fatalf("SetModel error: %v", err)
	}
	req := <-got
	if req.Method != "session/set_model" {
		t.Errorf("method = %q", req.Method)
	}
	var params SetModelRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.SessionID != "sess-1" || params.ModelID != "gemini-4-argon-low" {
		t.Errorf("params = %+v", params)
	}
}

func TestACPProcess_SetModel_Error(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	defer serverW.Close()
	defer serverR.Close()

	done := replyError(serverR, serverW, -32602, "unknown model")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := proc.SetModel(ctx, "s", "bad")
	if err == nil || !strings.Contains(err.Error(), "acp session/set_model") {
		t.Fatalf("err = %v", err)
	}
	<-done
}

func TestACPProcess_NewSessionWithMeta_SendsMeta(t *testing.T) {
	proc, serverW, serverR := buildACPProcess(nil, nil)
	defer serverW.Close()
	defer serverR.Close()

	got := make(chan jsonrpcMessage, 1)
	go func() {
		buf := make([]byte, 32*1024)
		n, err := serverR.Read(buf)
		if err != nil {
			return
		}
		var req jsonrpcMessage
		if json.Unmarshal(buf[:n], &req) != nil {
			return
		}
		got <- req
		data, _ := json.Marshal(jsonrpcMessage{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"sessionId":"s1"}`)})
		serverW.Write(append(data, '\n'))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sid, err := proc.NewSessionWithMeta(ctx, json.RawMessage(`{"antigravity":{"systemPrompt":{"append":"qa only"}}}`), nil)
	if err != nil || sid != "s1" {
		t.Fatalf("sid=%q err=%v", sid, err)
	}
	req := <-got
	var params NewSessionRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(params.Meta), "qa only") {
		t.Fatalf("meta not sent: %s", params.Meta)
	}
}
