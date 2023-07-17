package shell

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/replicate/cog/pkg/util/console"
)

func WaitForPort(port int, timeout time.Duration) error {
	start := time.Now()
	for {
		if PortIsOpen(port) {
			return nil
		}

		now := time.Now()
		if now.Sub(start) > timeout {
			return fmt.Errorf("Timed out")
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func WaitForHTTPOK(url string, timeout time.Duration) error {
	start := time.Now()
	console.Debugf("Waiting for %s to become accessible", url)
	for {
		now := time.Now()
		if now.Sub(start) > timeout {
			return fmt.Errorf("Timed out")
		}

		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(url) //#nosec G107
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			continue
		}
		console.Debugf("Got successful response from %s", url)
		return nil
	}
}

func PortIsOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("", strconv.Itoa(port)), 100*time.Millisecond)
	if conn != nil {
		conn.Close()
	}
	return err == nil
}
