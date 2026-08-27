// Package ports allocates the free ports an offline kc.sh export binds.
//
// This is the mechanism behind FR-C10. Offline export starts an embedded
// Keycloak runtime; if the machine is already serving on 8080/8443/9000 the
// export exits non-zero and the capture fails for a reason that has nothing to
// do with the realm.
package ports

import (
	"context"
	"fmt"
	"net"

	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/target"
)

// Allocate asks the operating system for three unused ports.
//
// It binds :0, records what the OS assigned, and releases immediately. There is
// an unavoidable race between releasing a port and the export claiming it —
// which is why a bind conflict is classified retryable and the whole allocation
// is simply redone with fresh ports.
func Allocate() (target.PortSet, error) {
	var (
		set       target.PortSet
		listeners []net.Listener
	)
	// The listeners are all held open at once, so the OS cannot hand out the
	// same port three times.
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()

	for i := 0; i < 3; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return target.PortSet{}, resil.Retry("allocate a free port",
				"PortCloak could not find a free port to run the export on.", err)
		}
		listeners = append(listeners, l)
		port := l.Addr().(*net.TCPAddr).Port
		switch i {
		case 0:
			set.HTTP = port
		case 1:
			set.HTTPS = port
		case 2:
			set.Management = port
		}
	}
	return set, nil
}

// Free reports whether a specific port is currently bindable, which is what a
// probe uses to say something concrete about a target.
func Free(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// RemoteAllocator asks a remote host for free ports. SSH supplies one: the same
// problem exists on a bastioned host and is harder to see there.
type RemoteAllocator interface {
	AllocateRemote(ctx context.Context) (target.PortSet, error)
}

// IsBindConflict recognises the exit output of an export that lost the race.
// Being wrong here is cheap in one direction and expensive in the other: a
// missed conflict fails a capture that would have worked on a retry.
func IsBindConflict(stderr string) bool {
	for _, s := range []string{
		"Address already in use",
		"address already in use",
		"Failed to start quarkus",
		"java.net.BindException",
		"Port already bound",
		"Unable to start HTTP server",
	} {
		if contains(stderr, s) {
			return true
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
