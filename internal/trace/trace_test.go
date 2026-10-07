package trace

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// Missing or reordered events and a counter that increments on entry break this transcript.
func TestWriterTranscript(t *testing.T) {
	var output bytes.Buffer
	writer := NewWriter(&output)
	steps := []func() error{
		func() error { return writer.Setup(65536, 0) },
		writer.Entry,
		func() error { return writer.IOExit("OUT", 0x00e9, 'H') },
		writer.Entry,
		func() error { return writer.IOExit("OUT", 0x00e9, 'i') },
		writer.Entry,
		func() error { return writer.IOExit("IN", 0x5050, 42) },
		writer.Entry,
		func() error { return writer.IOExit("OUT", 0x00f4, 42) },
		writer.Entry, writer.HLT,
		func() error { return writer.Summary("Hi", 42, 42) },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	want := "guest RAM: 65536 bytes\nvCPU: 0\n" +
		"[vm-entry]\n[vm-exit] IO OUT port=0x00e9 value=0x48 ('H') exits=1\n" +
		"[vm-entry]\n[vm-exit] IO OUT port=0x00e9 value=0x69 ('i') exits=2\n" +
		"[vm-entry]\n[vm-exit] IO IN  port=0x5050 value=0x2a ('*') exits=3\n" +
		"[vm-entry]\n[vm-exit] IO OUT port=0x00f4 value=0x2a ('*') exits=4\n" +
		"[vm-entry]\n[vm-exit] HLT exits=5\n" +
		"guest console: Hi\nhost input: 42\nguest result: 42\nstatus: PASS\n"
	if got := output.String(); got != want {
		t.Errorf("transcript =\n%s\nwant:\n%s", got, want)
	}
}

// Restricting ASCII too broadly or printing control characters breaks byte rendering.
func TestIOExitPrintableASCII(t *testing.T) {
	for _, tc := range []struct {
		value byte
		want  string
	}{
		{0x1f, "[vm-exit] IO OUT port=0x00e9 value=0x1f exits=1\n"},
		{0x20, "[vm-exit] IO OUT port=0x00e9 value=0x20 (' ') exits=1\n"},
		{0x7e, "[vm-exit] IO OUT port=0x00e9 value=0x7e ('~') exits=1\n"},
		{0x7f, "[vm-exit] IO OUT port=0x00e9 value=0x7f exits=1\n"},
		{0xff, "[vm-exit] IO OUT port=0x00e9 value=0xff exits=1\n"},
	} {
		var output bytes.Buffer
		if err := NewWriter(&output).IOExit("OUT", 0x00e9, tc.value); err != nil {
			t.Fatal(err)
		}
		if got := output.String(); got != tc.want {
			t.Errorf("byte %#x: got %q, want %q", tc.value, got, tc.want)
		}
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// Swallowing any writer error would make the CLI silently report success.
func TestWriterPropagatesErrors(t *testing.T) {
	sentinel := errors.New("trace output failed")
	for _, tc := range []struct {
		name string
		emit func(*Writer) error
	}{
		{"setup", func(w *Writer) error { return w.Setup(65536, 0) }},
		{"entry", (*Writer).Entry},
		{"IO exit", func(w *Writer) error { return w.IOExit("OUT", 0xe9, 'H') }},
		{"HLT", (*Writer).HLT},
		{"summary", func(w *Writer) error { return w.Summary("Hi", 42, 42) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.emit(NewWriter(failingWriter{sentinel})); !errors.Is(err, sentinel) {
				t.Errorf("error = %v, want %v", err, sentinel)
			}
		})
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestWriterRejectsShortWrite(t *testing.T) {
	if err := NewWriter(shortWriter{}).Entry(); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("error = %v, want ErrShortWrite", err)
	}
}
