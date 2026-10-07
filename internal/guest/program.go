// Package guest provides the raw x86 programs used by the IronVM demo.
package guest

// demoProgram is real-mode x86 machine code implementing the M2 port-I/O
// protocol. It is kept as bytes so the demo does not require an assembler.
//
// Assembly-style listing:
//
//	BA E9 00       mov dx, 0x00E9
//	B0 48          mov al, 'H'
//	EE             out dx, al
//	B0 69          mov al, 'i'
//	EE             out dx, al
//	BA 50 50       mov dx, 0x5050
//	EC             in al, dx
//	BA F4 00       mov dx, 0x00F4
//	EE             out dx, al
//	F4             hlt
//
// Opcode references: Intel 64 and IA-32 Architectures Software Developer's
// Manual, Vol. 2A (MOV, IN, HLT) and Vol. 2B (OUT):
// https://cdrdv2-public.intel.com/782156/325383-sdm-vol-2abcd.pdf
var demoProgram = []byte{
	0xBA, 0xE9, 0x00,
	0xB0, 0x48,
	0xEE,
	0xB0, 0x69,
	0xEE,
	0xBA, 0x50, 0x50,
	0xEC,
	0xBA, 0xF4, 0x00,
	0xEE,
	0xF4,
}

// DemoProgram returns the raw x86 demo guest code. The returned slice is a
// copy, so callers may modify it without changing subsequent results.
func DemoProgram() []byte {
	return append([]byte(nil), demoProgram...)
}
