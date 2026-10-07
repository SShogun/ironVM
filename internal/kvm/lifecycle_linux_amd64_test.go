//go:build linux && amd64

package kvm

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// A failed munmap must retain ownership and allow retry without closing a
// possibly reused fd or removing the already removed memory slot again.
func TestVMCloseRetriesFailedUnmap(t *testing.T) {
	memory := testMapping(t)
	fd, releaseFD := testFD(t)
	vm := &VM{fd: fd, memory: memory}
	unmapErr := errors.New("guest unmap failed")
	closeErr := errors.New("VM close failed")
	var calls []string
	remove := func(fd int) error {
		calls = append(calls, "remove slot")
		if fd != vm.fd || len(vm.memory) == 0 {
			t.Fatal("slot removal lost its fd or live guest mapping")
		}
		return nil
	}
	closeFD := func(fd int) error {
		calls = append(calls, "close fd")
		if err := releaseFD(fd); err != nil {
			t.Fatal(err)
		}
		// On Linux a close error does not mean the fd may safely be retried.
		return closeErr
	}
	fail := true
	unmap := func(mapping []byte) error {
		calls = append(calls, "unmap")
		if fail {
			return unmapErr
		}
		return syscall.Munmap(mapping)
	}
	if err := vm.closeWith(remove, closeFD, unmap); !errors.Is(err, unmapErr) || !errors.Is(err, closeErr) {
		t.Fatalf("first Close error = %v, want both close and unmap failures", err)
	}
	if vm.closed {
		t.Error("VM marked fully closed while guest unmap is pending")
	}
	if len(vm.memory) != len(memory) {
		t.Fatal("failed unmap lost guest mapping")
	}
	if err := vm.LoadGuest(0, []byte{0xf4}); !errors.Is(err, errVMClosed) {
		t.Errorf("LoadGuest after fd release = %v, want closed", err)
	}
	if _, err := vm.NewVCPU(0); err == nil || !strings.Contains(err.Error(), "VM is closed") {
		t.Errorf("NewVCPU after fd release = %v, want closed", err)
	}
	if err := vm.closeWith(remove, closeFD, unmap); !errors.Is(err, unmapErr) {
		t.Fatalf("retry Close error = %v, want retained unmap failure", err)
	}
	fail = false
	if err := vm.closeWith(remove, closeFD, unmap); err != nil {
		t.Fatalf("successful unmap retry = %v", err)
	}
	if !vm.closed || vm.memory != nil {
		t.Error("successful unmap did not mark VM fully closed")
	}
	if err := vm.closeWith(remove, closeFD, unmap); !errors.Is(err, errVMClosed) {
		t.Errorf("duplicate Close = %v, want already closed", err)
	}
	want := []string{"remove slot", "close fd", "unmap", "unmap", "unmap"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("cleanup order = %v, want %v", calls, want)
	}
}

func TestVCPUCloseRetriesFailedUnmap(t *testing.T) {
	mapping := testMapping(t)
	fd, releaseFD := testFD(t)
	vcpu := &VCPU{fd: fd, run: mapping}
	unmapErr := errors.New("run unmap failed")
	closeErr := errors.New("vCPU close failed")
	var calls []string
	closeFD := func(fd int) error {
		calls = append(calls, "close fd")
		if err := releaseFD(fd); err != nil {
			t.Fatal(err)
		}
		return closeErr
	}
	fail := true
	unmap := func(mapping []byte) error {
		calls = append(calls, "unmap")
		if fail {
			return unmapErr
		}
		return syscall.Munmap(mapping)
	}
	if err := vcpu.closeWith(closeFD, unmap); !errors.Is(err, unmapErr) || !errors.Is(err, closeErr) {
		t.Fatalf("first Close error = %v, want both close and unmap failures", err)
	}
	if vcpu.closed {
		t.Error("vCPU marked fully closed while run unmap is pending")
	}
	if len(vcpu.run) != len(mapping) {
		t.Error("failed unmap lost run mapping")
	}
	if _, err := vcpu.Run(); err == nil || !strings.Contains(err.Error(), "vCPU is closed") {
		t.Errorf("Run after close attempt = %v, want closed", err)
	}
	if err := vcpu.ConfigureRealMode(0); err == nil || !strings.Contains(err.Error(), "vCPU is closed") {
		t.Errorf("ConfigureRealMode after close attempt = %v, want closed", err)
	}
	if err := vcpu.CompleteIO(Exit{}, nil); err == nil || !strings.Contains(err.Error(), "vCPU is closed") {
		t.Errorf("CompleteIO after close attempt = %v, want closed", err)
	}
	if err := vcpu.closeWith(closeFD, unmap); !errors.Is(err, unmapErr) {
		t.Fatalf("retry Close error = %v, want retained unmap failure", err)
	}
	fail = false
	if err := vcpu.closeWith(closeFD, unmap); err != nil {
		t.Fatalf("successful unmap retry = %v", err)
	}
	if !vcpu.closed || vcpu.run != nil {
		t.Error("successful unmap did not mark vCPU fully closed")
	}
	if err := vcpu.closeWith(closeFD, unmap); err == nil {
		t.Error("duplicate Close succeeded")
	}
	want := []string{"unmap", "close fd", "unmap", "unmap"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("cleanup order = %v, want %v", calls, want)
	}
}

func TestVMCloseSlotRemovalFailureRetainsResourcesAndStopsOperations(t *testing.T) {
	fd, releaseFD := testFD(t)
	vm := &VM{fd: fd, memory: testMapping(t), runMmapSize: 4096}
	slotErr := errors.New("slot removal failed")
	var calls []string
	fail := true
	remove := func(fd int) error {
		calls = append(calls, "remove slot")
		if fd != vm.fd || len(vm.memory) == 0 {
			t.Fatal("slot removal lost its fd or live guest mapping")
		}
		if fail {
			return slotErr
		}
		return nil
	}
	closeFD := func(fd int) error {
		calls = append(calls, "close fd")
		return releaseFD(fd)
	}
	unmap := func(mapping []byte) error {
		calls = append(calls, "unmap")
		return syscall.Munmap(mapping)
	}
	if err := vm.closeWith(remove, closeFD, unmap); !errors.Is(err, slotErr) {
		t.Fatalf("Close error = %v, want slot removal failure", err)
	}
	if vm.closed || len(vm.memory) != 4096 || !reflect.DeepEqual(calls, []string{"remove slot"}) {
		t.Fatalf("slot failure released resources: closed=%t memory=%d calls=%v", vm.closed, len(vm.memory), calls)
	}
	if err := vm.LoadGuest(0, []byte{0xf4}); !errors.Is(err, errVMClosed) {
		t.Errorf("LoadGuest during teardown = %v, want closed", err)
	}
	if _, err := vm.NewVCPU(0); err == nil || !strings.Contains(err.Error(), "VM is closed") {
		t.Errorf("NewVCPU during teardown = %v, want closed", err)
	}
	fail = false
	if err := vm.closeWith(remove, closeFD, unmap); err != nil {
		t.Fatalf("Close retry = %v", err)
	}
	if !vm.closed || vm.memory != nil {
		t.Error("successful cleanup did not mark VM fully closed")
	}
	want := []string{"remove slot", "remove slot", "close fd", "unmap"}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("cleanup order = %v, want %v", calls, want)
	}
}

func testMapping(t *testing.T) []byte {
	t.Helper()
	mapping, err := syscall.Mmap(-1, 0, 4096, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		t.Fatal(err)
	}
	// Fallback cleanup also runs after a failed assertion. Ignore EINVAL after
	// the production cleanup has already unmapped it successfully.
	t.Cleanup(func() { _ = syscall.Munmap(mapping) })
	return mapping
}

func testFD(t *testing.T) (int, func(int) error) {
	t.Helper()
	fd, err := syscall.Open("/dev/null", syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	release := func(got int) error {
		if got != fd || released {
			t.Fatalf("invalid or repeated fd release: fd=%d want=%d released=%t", got, fd, released)
		}
		released = true
		return syscall.Close(got)
	}
	t.Cleanup(func() {
		if !released {
			_ = release(fd)
		}
	})
	return fd, release
}

// These checks protect the unchanged overflow-safe LoadGuest bounds while the
// lifecycle changes add a closing state to the same entry point.
func TestVMLoadGuestBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gpa     uint64
		code    []byte
		want    []byte
		wantErr bool
	}{
		{"whole RAM", 0, []byte{1, 2, 3, 4}, []byte{1, 2, 3, 4}, false},
		{"last byte", 3, []byte{42}, []byte{0, 0, 0, 42}, false},
		{"empty at end", 4, nil, []byte{0, 0, 0, 0}, false},
		{"nonempty at end", 4, []byte{42}, []byte{0, 0, 0, 0}, true},
		{"past RAM", 5, nil, []byte{0, 0, 0, 0}, true},
		{"maximum GPA", ^uint64(0), []byte{42}, []byte{0, 0, 0, 0}, true},
		{"code larger than RAM", 0, []byte{1, 2, 3, 4, 5}, []byte{0, 0, 0, 0}, true},
		{"code overruns tail", 1, []byte{1, 2, 3, 4}, []byte{0, 0, 0, 0}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := &VM{memory: make([]byte, 4)}
			if err := vm.LoadGuest(tc.gpa, tc.code); (err != nil) != tc.wantErr {
				t.Fatalf("LoadGuest(%d, %v) = %v, wantErr=%t", tc.gpa, tc.code, err, tc.wantErr)
			}
			if !bytes.Equal(vm.memory, tc.want) {
				t.Errorf("guest memory = %v, want %v", vm.memory, tc.want)
			}
		})
	}
}

func TestConstructionCleanupPreservesAllErrors(t *testing.T) {
	cause := errors.New("construction failed")
	err := closeAfterFailure(-1, cause)
	if !errors.Is(err, cause) || !errors.Is(err, syscall.EBADF) {
		t.Errorf("system cleanup = %v, want construction and close errors", err)
	}
	// A non-mmap slice makes the real syscall package reject Munmap without
	// altering a mapping; an invalid fd also makes the real Close fail.
	err = cleanupVMConstruction(-1, make([]byte, 4096), cause)
	if !errors.Is(err, cause) || !errors.Is(err, syscall.EBADF) || !errors.Is(err, syscall.EINVAL) {
		t.Errorf("VM cleanup = %v, want construction, close, and unmap errors", err)
	}
}
