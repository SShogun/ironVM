package device

import "testing"

func TestPortIOConsoleOutput(t *testing.T) {
	portIO := NewPortIO()

	for _, value := range []byte{'H', 'i'} {
		if err := portIO.Write(0x00E9, value); err != nil {
			t.Fatalf("Write(0x00E9, %q) error = %v", value, err)
		}
	}

	if got := portIO.Console(); got != "Hi" {
		t.Fatalf("Console() = %q, want %q", got, "Hi")
	}
}

func TestPortIOHostInput(t *testing.T) {
	portIO := NewPortIO()

	got, err := portIO.Read(0x5050)
	if err != nil {
		t.Fatalf("Read(0x5050) error = %v", err)
	}
	if got != 42 {
		t.Fatalf("Read(0x5050) = %d, want 42", got)
	}
}

func TestPortIOGuestResult(t *testing.T) {
	portIO := NewPortIO()

	if got, seen := portIO.Result(); seen {
		t.Fatalf("Result() before guest output = (%d, %t), want unseen", got, seen)
	}
	if err := portIO.Write(0x00F4, 42); err != nil {
		t.Fatalf("Write(0x00F4, 42) error = %v", err)
	}
	if got, seen := portIO.Result(); !seen || got != 42 {
		t.Fatalf("Result() = (%d, %t), want (42, true)", got, seen)
	}
}

func TestPortIORejectsUnsupportedPorts(t *testing.T) {
	portIO := NewPortIO()

	if _, err := portIO.Read(0x1234); err == nil {
		t.Fatal("Read(0x1234) error = nil, want unsupported-port error")
	}
	if err := portIO.Write(0x1234, 0xAA); err == nil {
		t.Fatal("Write(0x1234, 0xAA) error = nil, want unsupported-port error")
	}
}

func TestPortIORejectsUnsupportedDirections(t *testing.T) {
	portIO := NewPortIO()

	if _, err := portIO.Read(0x00E9); err == nil {
		t.Fatal("Read(0x00E9) error = nil, want unsupported-direction error")
	}
	if err := portIO.Write(0x5050, 42); err == nil {
		t.Fatal("Write(0x5050, 42) error = nil, want unsupported-direction error")
	}
}
