package mcpserver

import (
	"errors"
	"testing"

	"github.com/narglc/stock.quot.tele.bot/sender"
)

type mockSender struct {
	name     string
	lastText string
	id       string
	err      error
}

func (m *mockSender) Name() string { return m.name }
func (m *mockSender) Send(md string) (string, error) {
	m.lastText = md
	return m.id, m.err
}

func TestDispatch_OK(t *testing.T) {
	m := &mockSender{name: "mockok", id: "7"}
	sender.Register(m)

	id, err := dispatch("mockok", "hello")
	if err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if id != "7" {
		t.Errorf("id = %q, want 7", id)
	}
	if m.lastText != "hello" {
		t.Errorf("lastText = %q, want hello", m.lastText)
	}
}

func TestDispatch_UnknownPlatform(t *testing.T) {
	if _, err := dispatch("ghost", "hi"); err == nil {
		t.Error("unknown platform should error")
	}
}

func TestDispatch_SendError(t *testing.T) {
	sender.Register(&mockSender{name: "mockerr", err: errors.New("boom")})
	if _, err := dispatch("mockerr", "hi"); err == nil {
		t.Error("send error should propagate")
	}
}
