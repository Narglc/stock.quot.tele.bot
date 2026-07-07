package sender

import (
	"errors"
	"testing"

	tele "gopkg.in/telebot.v3"
)

type call struct {
	what interface{}
	html bool
}

type fakeBot struct {
	failHTML bool
	calls    []call
}

func (f *fakeBot) Send(to tele.Recipient, what interface{}, opts ...interface{}) (*tele.Message, error) {
	isHTML := false
	for _, o := range opts {
		if o == tele.ModeHTML {
			isHTML = true
		}
	}
	f.calls = append(f.calls, call{what: what, html: isHTML})
	if isHTML && f.failHTML {
		return nil, errors.New("bad html")
	}
	return &tele.Message{ID: 42}, nil
}

func TestTelegramSend_HTML(t *testing.T) {
	fb := &fakeBot{}
	s := NewTelegramSender(fb, 100)

	id, err := s.Send("**hi**")
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q, want 42", id)
	}
	if len(fb.calls) != 1 || !fb.calls[0].html {
		t.Fatalf("want 1 HTML call, got %+v", fb.calls)
	}
	if fb.calls[0].what != "<b>hi</b>" {
		t.Errorf("what = %q, want <b>hi</b>", fb.calls[0].what)
	}
}

func TestTelegramSend_Downgrade(t *testing.T) {
	fb := &fakeBot{failHTML: true}
	s := NewTelegramSender(fb, 100)

	id, err := s.Send("**hi**")
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q, want 42", id)
	}
	if len(fb.calls) != 2 {
		t.Fatalf("want 2 calls (html fail + plain), got %d", len(fb.calls))
	}
	if fb.calls[1].html {
		t.Error("second call should be plain text, not HTML")
	}
	if fb.calls[1].what != "**hi**" {
		t.Errorf("fallback what = %q, want raw markdown **hi**", fb.calls[1].what)
	}
}

func TestTelegramName(t *testing.T) {
	if (&TelegramSender{}).Name() != "telegram" {
		t.Error("Name should be telegram")
	}
}
