package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const sessionPermParams = `{"sessionId":"s","toolCall":{"toolCallId":"t","title":"%TITLE%","kind":"%KIND%"},"options":[{"optionId":"a1","name":"Allow once","kind":"allow_once"},{"optionId":"a2","name":"Always","kind":"allow_always"},{"optionId":"r1","name":"Reject","kind":"reject_once"}]}`

func sessionPerm(t *testing.T, tb *ToolBridge, title, kind string) SessionPermissionOutcome {
	t.Helper()
	p := strings.NewReplacer("%TITLE%", title, "%KIND%", kind).Replace(sessionPermParams)
	out, err := tb.Handle(context.Background(), "session/request_permission", json.RawMessage(p))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return out.(*SessionPermissionResponse).Outcome
}

func TestSessionPermission_ApproveAll(t *testing.T) {
	tb, _ := newTestBridge(t, WithClientToolsDisabled(), WithPermMode("approve-all"))
	got := sessionPerm(t, tb, "run ls", "execute")
	if got.Outcome != "selected" || got.OptionID != "a1" {
		t.Errorf("got %+v", got)
	}
}

func TestSessionPermission_DenyAll(t *testing.T) {
	tb, _ := newTestBridge(t, WithClientToolsDisabled(), WithPermMode("deny-all"))
	got := sessionPerm(t, tb, "read file", "read")
	if got.Outcome != "selected" || got.OptionID != "r1" {
		t.Errorf("got %+v", got)
	}
}

func TestSessionPermission_ApproveReads(t *testing.T) {
	tb, _ := newTestBridge(t, WithClientToolsDisabled(), WithPermMode("approve-reads"))
	if got := sessionPerm(t, tb, "view x", "read"); got.OptionID != "a1" {
		t.Errorf("read: %+v", got)
	}
	if got := sessionPerm(t, tb, "run ls", "execute"); got.OptionID != "r1" {
		t.Errorf("execute: %+v", got)
	}
}

func TestSessionPermission_NoMatchingOptionCancels(t *testing.T) {
	tb, _ := newTestBridge(t, WithClientToolsDisabled(), WithPermMode("deny-all"))
	out, err := tb.Handle(context.Background(), "session/request_permission",
		json.RawMessage(`{"sessionId":"s","toolCall":{},"options":[{"optionId":"a1","kind":"allow_once"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(*SessionPermissionResponse).Outcome.Outcome; got != "cancelled" {
		t.Errorf("outcome = %q", got)
	}
}

func TestSessionPermission_ResponseShape(t *testing.T) {
	tb, _ := newTestBridge(t, WithClientToolsDisabled())
	out, _ := tb.Handle(context.Background(), "session/request_permission", json.RawMessage(strings.NewReplacer("%TITLE%", "x", "%KIND%", "edit").Replace(sessionPermParams)))
	b, _ := json.Marshal(out)
	if string(b) != `{"outcome":{"outcome":"selected","optionId":"a1"}}` {
		t.Errorf("json = %s", b)
	}
}

func TestLegacyPermissionRequest_Unchanged(t *testing.T) {
	tb, _ := newTestBridge(t)
	out, err := tb.Handle(context.Background(), "permission/request", json.RawMessage(`{"toolName":"bash"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if string(b) != `{"outcome":"approved"}` {
		t.Errorf("json = %s", b)
	}
}

func TestClientToolsDisabled(t *testing.T) {
	tb, dir := newTestBridge(t, WithClientToolsDisabled())
	cases := map[string]string{
		"fs/readTextFile":  `{"path":"a.txt"}`,
		"fs/writeTextFile": `{"path":"a.txt","content":"x"}`,
		"terminal/create":  `{"command":"sh","args":["-c","touch ` + dir + `/pwned"]}`,
		"terminal/output":  `{"terminalId":"t"}`,
	}
	for method, params := range cases {
		if _, err := tb.Handle(context.Background(), method, json.RawMessage(params)); err == nil || !strings.Contains(err.Error(), "client tools disabled") {
			t.Errorf("%s: err = %v", method, err)
		}
	}
	if _, err := tb.Handle(context.Background(), "session/request_permission", json.RawMessage(`{"options":[]}`)); err != nil {
		t.Errorf("permission blocked: %v", err)
	}
	if _, err := tb.Handle(context.Background(), "permission/request", json.RawMessage(`{"toolName":"x"}`)); err != nil {
		t.Errorf("legacy permission blocked: %v", err)
	}
}

func TestSessionPermission_GenericACPUnknownMethod(t *testing.T) {
	for _, mode := range []string{"", "approve-all", "approve-reads", "deny-all"} {
		tb, _ := newTestBridge(t, WithPermMode(mode))
		out, err := tb.Handle(context.Background(), "session/request_permission", json.RawMessage(strings.NewReplacer("%TITLE%", "x", "%KIND%", "execute").Replace(sessionPermParams)))
		if err == nil || !strings.Contains(err.Error(), "unknown method") || out != nil {
			t.Errorf("mode %q: out=%v err=%v", mode, out, err)
		}
	}
}

func TestSessionPermission_ApproveReadsTrustsKind(t *testing.T) {
	tb, _ := newTestBridge(t, WithClientToolsDisabled(), WithPermMode("approve-reads"))
	if got := sessionPerm(t, tb, "grep logs", "execute"); got.OptionID != "r1" {
		t.Errorf("execute with read-like title: %+v", got)
	}
	if got := sessionPerm(t, tb, "view x", "other"); got.OptionID != "a1" {
		t.Errorf("other with read title: %+v", got)
	}
	if got := sessionPerm(t, tb, "write x", ""); got.OptionID != "r1" {
		t.Errorf("empty kind write title: %+v", got)
	}
}
