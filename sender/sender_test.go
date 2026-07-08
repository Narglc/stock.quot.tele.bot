package sender

import "testing"

type stubSender struct{ name string }

func (s stubSender) Name() string                        { return s.name }
func (s stubSender) Send(string, string) (string, error) { return "1", nil }

func TestRegistry(t *testing.T) {
	Register(stubSender{name: "stub"})

	got, ok := Get("stub")
	if !ok {
		t.Fatal("Get(stub) not found after Register")
	}
	if got.Name() != "stub" {
		t.Errorf("Name = %q, want stub", got.Name())
	}
	if _, ok := Get("nope"); ok {
		t.Error("Get(nope) should be false")
	}
}
