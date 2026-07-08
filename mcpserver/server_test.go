package mcpserver

import (
	"errors"
	"testing"

	"github.com/narglc/stock.quot.tele.bot/sender"
)

type mockSender struct {
	name     string
	lastText string
	lastTo   string
	id       string
	err      error
}

func (m *mockSender) Name() string { return m.name }
func (m *mockSender) Send(md, recipient string) (string, error) {
	m.lastText = md
	m.lastTo = recipient
	return m.id, m.err
}

func TestDispatch_OK(t *testing.T) {
	m := &mockSender{name: "mockok", id: "7"}
	sender.Register(m)

	id, err := dispatch("mockok", "hello", "100")
	if err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if id != "7" {
		t.Errorf("id = %q, want 7", id)
	}
	if m.lastText != "hello" {
		t.Errorf("lastText = %q, want hello", m.lastText)
	}
	if m.lastTo != "100" {
		t.Errorf("lastTo = %q, want 100", m.lastTo)
	}
}

func TestDispatch_UnknownPlatform(t *testing.T) {
	if _, err := dispatch("ghost", "hi", "1"); err == nil {
		t.Error("unknown platform should error")
	}
}

func TestDispatch_SendError(t *testing.T) {
	sender.Register(&mockSender{name: "mockerr", err: errors.New("boom")})
	if _, err := dispatch("mockerr", "hi", "1"); err == nil {
		t.Error("send error should propagate")
	}
}

func TestJWT_RoundTrip(t *testing.T) {
	secret := "s3cr3t"
	tok, err := SignToken(secret, 6225152335)
	if err != nil {
		t.Fatalf("SignToken err: %v", err)
	}

	chatID, err := parseBearer(secret, "Bearer "+tok)
	if err != nil {
		t.Fatalf("parseBearer err: %v", err)
	}
	if chatID != 6225152335 {
		t.Errorf("chatID = %d, want 6225152335", chatID)
	}
}

func TestJWT_WrongSecret(t *testing.T) {
	tok, _ := SignToken("right", 100)
	if _, err := parseBearer("wrong", "Bearer "+tok); err == nil {
		t.Error("wrong secret should fail verification")
	}
}

func TestJWT_MissingBearer(t *testing.T) {
	if _, err := parseBearer("s", ""); err == nil {
		t.Error("empty header should error")
	}
	if _, err := parseBearer("s", "Basic xxx"); err == nil {
		t.Error("non-bearer header should error")
	}
}

func TestSignToken_EmptySecret(t *testing.T) {
	if _, err := SignToken("", 100); err == nil {
		t.Error("empty secret should refuse to sign")
	}
}
