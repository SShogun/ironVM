// Package trace formats the deterministic guest execution transcript.
package trace

import (
	"fmt"
	"io"
)

// Writer streams execution events and counts the observed VM exits.
// A Writer belongs to one execution and is not safe for concurrent use.
type Writer struct {
	output io.Writer
	exits  int
}

func NewWriter(output io.Writer) *Writer { return &Writer{output: output} }

func (w *Writer) write(format string, args ...any) error {
	text := fmt.Sprintf(format, args...)
	n, err := io.WriteString(w.output, text)
	if err != nil {
		return err
	}
	if n != len(text) {
		return io.ErrShortWrite
	}
	return nil
}

func (w *Writer) Setup(ramBytes, vcpuID int) error {
	return w.write("guest RAM: %d bytes\nvCPU: %d\n", ramBytes, vcpuID)
}

func (w *Writer) Entry() error { return w.write("[vm-entry]\n") }

func (w *Writer) IOExit(direction string, port uint16, value byte) error {
	w.exits++
	ascii := ""
	if value >= 0x20 && value <= 0x7e {
		ascii = fmt.Sprintf(" (%q)", rune(value))
	}
	return w.write("[vm-exit] IO %-3s port=0x%04x value=0x%02x%s exits=%d\n", direction, port, value, ascii, w.exits)
}

func (w *Writer) HLT() error {
	w.exits++
	return w.write("[vm-exit] HLT exits=%d\n", w.exits)
}

// Summary is emitted only after the VMM validates the guest protocol.
func (w *Writer) Summary(console string, hostInput, guestResult byte) error {
	return w.write("guest console: %s\nhost input: %d\nguest result: %d\nstatus: PASS\n", console, hostInput, guestResult)
}
