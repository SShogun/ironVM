package device

import "fmt"

const (
	portDebugConsole uint16 = 0x00E9
	portHostInput    uint16 = 0x5050
	portGuestResult  uint16 = 0x00F4
)

// PortIO implements the small port-I/O protocol used by the IronVM guest.
// It has no dependency on KVM; the caller handles KVM exit decoding and
// passes individual byte operations here.
type PortIO struct {
	console []byte
	result  byte
	seen    bool
}

// NewPortIO creates an empty port-I/O device.
func NewPortIO() *PortIO {
	return &PortIO{}
}

// Read returns the host-provided byte for a supported guest input port.
func (p *PortIO) Read(port uint16) (byte, error) {
	if p == nil {
		return 0, fmt.Errorf("port I/O IN 0x%04X: nil device", port)
	}
	switch port {
	case portHostInput:
		return 42, nil
	case portDebugConsole, portGuestResult:
		return 0, fmt.Errorf("port I/O IN 0x%04X: unsupported direction", port)
	default:
		return 0, fmt.Errorf("port I/O IN 0x%04X: unsupported port", port)
	}
}

// Write handles a supported guest output port.
func (p *PortIO) Write(port uint16, value byte) error {
	if p == nil {
		return fmt.Errorf("port I/O OUT 0x%04X: nil device", port)
	}
	switch port {
	case portDebugConsole:
		p.console = append(p.console, value)
	case portGuestResult:
		p.result = value
		p.seen = true
	case portHostInput:
		return fmt.Errorf("port I/O OUT 0x%04X: unsupported direction", port)
	default:
		return fmt.Errorf("port I/O OUT 0x%04X: unsupported port", port)
	}
	return nil
}

// Console returns the bytes written by the guest to port 0xE9.
func (p *PortIO) Console() string {
	if p == nil {
		return ""
	}
	return string(p.console)
}

// Result returns the guest-reported byte and whether the guest wrote it.
func (p *PortIO) Result() (byte, bool) {
	if p == nil {
		return 0, false
	}
	return p.result, p.seen
}
