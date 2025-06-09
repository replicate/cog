package util

import (
	"fmt"
	"math/rand"
	"net"
	"time"
)

// PickFreePort returns a TCP port in [min,max] that's not in use on the 127.0.0.1 interface.
// Note that there's a small chance of a race condition when a port is considered free at the
// time of the call, but not free when something tries to use it. This is good enough for dev
// and test code though.
func PickFreePort(minPort, maxPort int) (int, error) {
	if minPort < 1024 || maxPort > 99999 || minPort > maxPort {
		return 0, fmt.Errorf("invalid port range")
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano())) // #nosec G404 - using math/rand is fine for test port selection
	for range 20 {                                         // avoid infinite loops
		p := rng.Intn(maxPort-minPort+1) + minPort
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			l.Close()
			return p, nil // looks free
		}
	}
	return 0, fmt.Errorf("could not find free port in range %d-%d", minPort, maxPort)
}
