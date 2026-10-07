//go:build linux && amd64

package kvm

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestDecodeExitIOOutCopiesPackedPayload(t *testing.T) {
	run := makeSyntheticIOExit(IODirectionOut, 0xE9, 1, 2, 64, []byte{'H', 'i'})

	exit, err := decodeExit(run)
	if err != nil {
		t.Fatalf("decodeExit() error = %v", err)
	}
	if exit.Reason != ExitReasonIO || exit.IO == nil {
		t.Fatalf("decodeExit() = %#v, want KVM_EXIT_IO", exit)
	}
	if exit.IO.Direction != IODirectionOut || exit.IO.Port != 0xE9 || exit.IO.Size != 1 || exit.IO.Count != 2 {
		t.Fatalf("decoded IO = %#v, want OUT port 0xE9 size 1 count 2", exit.IO)
	}
	if !bytes.Equal(exit.IO.Data, []byte{'H', 'i'}) {
		t.Fatalf("OUT data = %v, want %q", exit.IO.Data, "Hi")
	}
	run[64] = 'x'
	if !bytes.Equal(exit.IO.Data, []byte{'H', 'i'}) {
		t.Fatalf("OUT data aliases kvm_run mapping: %v", exit.IO.Data)
	}
}

func TestDecodeExitIOInHasNoPayloadUntilCompleted(t *testing.T) {
	run := makeSyntheticIOExit(IODirectionIn, 0x5050, 1, 1, 64, []byte{0})
	exit, err := decodeExit(run)
	if err != nil {
		t.Fatalf("decodeExit() error = %v", err)
	}
	if exit.Reason != ExitReasonIO || exit.IO == nil {
		t.Fatalf("decodeExit() = %#v, want KVM_EXIT_IO", exit)
	}
	if exit.IO.Direction != IODirectionIn || exit.IO.Port != 0x5050 || exit.IO.Size != 1 || exit.IO.Count != 1 {
		t.Fatalf("decoded IO = %#v, want IN port 0x5050 size 1 count 1", exit.IO)
	}
	if exit.IO.Data != nil {
		t.Fatalf("IN data = %v, want nil until host supplies a response", exit.IO.Data)
	}
}

func TestDecodeExitIORejectsPayloadOutsideMapping(t *testing.T) {
	tests := []struct {
		name       string
		offset     uint64
		size       uint8
		count      uint32
		wantErrMsg string
	}{
		{name: "offset beyond mapping", offset: 256, size: 1, count: 1, wantErrMsg: "exceeds"},
		{name: "payload runs past mapping", offset: 127, size: 1, count: 2, wantErrMsg: "exceeds"},
		{name: "offset overlaps metadata", offset: 47, size: 1, count: 1, wantErrMsg: "overlaps"},
		{name: "zero size", offset: 64, size: 0, count: 1, wantErrMsg: "dimensions"},
		{name: "zero count", offset: 64, size: 1, count: 0, wantErrMsg: "dimensions"},
		{name: "maximum offset", offset: ^uint64(0), size: 1, count: 1, wantErrMsg: "exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := makeSyntheticIOExit(IODirectionOut, 0xE9, test.size, test.count, test.offset, nil)
			_, err := decodeExit(run)
			if err == nil || !strings.Contains(err.Error(), test.wantErrMsg) {
				t.Fatalf("decodeExit() error = %v, want substring %q", err, test.wantErrMsg)
			}
		})
	}
}

func TestCompleteIOWritesExactResponseToMappedPayload(t *testing.T) {
	run := makeSyntheticIOExit(IODirectionIn, 0x5050, 2, 2, 64, []byte{0, 0, 0, 0})
	exit, err := decodeExit(run)
	if err != nil {
		t.Fatalf("decodeExit() error = %v", err)
	}
	vcpu := &VCPU{run: run, pendingIO: exit.IO}
	response := []byte{42, 43, 44, 45}
	if err := vcpu.CompleteIO(exit, response); err != nil {
		t.Fatalf("CompleteIO() error = %v", err)
	}
	if got := run[64:68]; !bytes.Equal(got, response) {
		t.Fatalf("mapped response = %v, want %v", got, response)
	}
	if vcpu.pendingIO != nil {
		t.Fatal("pendingIO not cleared after successful completion")
	}
}

func TestCompleteIORequiresExactResponseAndPendingIN(t *testing.T) {
	run := makeSyntheticIOExit(IODirectionIn, 0x5050, 1, 2, 64, []byte{0, 0})
	exit, err := decodeExit(run)
	if err != nil {
		t.Fatalf("decodeExit() error = %v", err)
	}
	vcpu := &VCPU{run: run, pendingIO: exit.IO}
	if err := vcpu.CompleteIO(exit, []byte{42}); err == nil {
		t.Fatal("CompleteIO() accepted a response with the wrong size")
	}
	if !bytes.Equal(run[64:66], []byte{0, 0}) {
		t.Fatalf("mapping changed after rejected response: %v", run[64:66])
	}
	if err := vcpu.CompleteIO(exit, []byte{42, 43}); err != nil {
		t.Fatalf("CompleteIO() error = %v", err)
	}
	if err := vcpu.CompleteIO(exit, []byte{42, 43}); err == nil {
		t.Fatal("CompleteIO() accepted an already completed exit")
	}
}

func TestCompleteIORejectsMutatedExitMetadata(t *testing.T) {
	run := makeSyntheticIOExit(IODirectionIn, 0x5050, 1, 1, 64, []byte{0})
	exit, err := decodeExit(run)
	if err != nil {
		t.Fatalf("decodeExit() error = %v", err)
	}
	pending := *exit.IO
	vcpu := &VCPU{run: run, pendingIO: &pending}
	exit.IO.Size = 2
	if err := vcpu.CompleteIO(exit, []byte{42, 43}); err == nil {
		t.Fatal("CompleteIO() accepted caller-mutated exit metadata")
	}
	if !bytes.Equal(run[64:65], []byte{0}) {
		t.Fatalf("mapping changed after rejected metadata: %v", run[64:65])
	}
}

func makeSyntheticIOExit(direction uint8, port uint16, size uint8, count uint32, dataOffset uint64, payload []byte) []byte {
	run := make([]byte, 128)
	binary.LittleEndian.PutUint32(run[kvmRunExitOffset:], ExitReasonIO)
	run[kvmRunIODirectionOffset] = direction
	run[kvmRunIOSizeOffset] = size
	binary.LittleEndian.PutUint16(run[kvmRunIOPortOffset:], port)
	binary.LittleEndian.PutUint32(run[kvmRunIOCountOffset:], count)
	binary.LittleEndian.PutUint64(run[kvmRunIODataOffset:], dataOffset)
	if dataOffset <= uint64(len(run)) && uint64(len(payload)) <= uint64(len(run))-dataOffset {
		copy(run[int(dataOffset):], payload)
	}
	return run
}
