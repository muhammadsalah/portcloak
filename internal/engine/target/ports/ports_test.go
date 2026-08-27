// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package ports

import (
	"net"
	"runtime"
	"testing"
)

func TestAllocate_ReturnsThreeDistinctFreePorts(t *testing.T) {
	set, err := Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if !set.Allocated() {
		t.Fatalf("allocation left a port unset: %+v", set)
	}
	if set.HTTP == set.HTTPS || set.HTTP == set.Management || set.HTTPS == set.Management {
		t.Fatalf("ports are not distinct: %+v", set)
	}
	// Every port must be bindable again once released — proof the allocator
	// did not leave its own sockets open.
	for _, p := range []int{set.HTTP, set.HTTPS, set.Management} {
		if !Free(p) {
			t.Errorf("port %d is still held after allocation returned", p)
		}
	}
}

func TestAllocate_DoesNotLeakFileDescriptors(t *testing.T) {
	// Allocating many times in a row would exhaust the descriptor table if the
	// listeners were not closed.
	for i := 0; i < 200; i++ {
		if _, err := Allocate(); err != nil {
			t.Fatalf("allocation %d failed, which suggests leaked sockets: %v", i, err)
		}
	}
	runtime.GC()
}

func TestFree_SeesAnOccupiedPort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	if Free(port) {
		t.Fatalf("port %d is occupied but reported free", port)
	}
}

// The race between releasing a port and Keycloak binding it is unavoidable, so
// the conflict has to be recognisable from the export's own output.
func TestIsBindConflict(t *testing.T) {
	conflicts := []string{
		"java.net.BindException: Address already in use",
		"ERROR: Unable to start HTTP server",
		"Failed to start quarkus",
	}
	for _, s := range conflicts {
		if !IsBindConflict(s) {
			t.Errorf("IsBindConflict(%q) = false", s)
		}
	}
	other := []string{
		"ERROR: Realm 'acme' not found",
		"Failed to obtain JDBC connection",
		"",
	}
	for _, s := range other {
		if IsBindConflict(s) {
			t.Errorf("IsBindConflict(%q) = true, which would retry a failure that will not fix itself", s)
		}
	}
}
