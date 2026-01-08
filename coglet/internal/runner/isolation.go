package runner

import (
	"errors"
	"os/user"
	"strconv"
	"sync"
)

const (
	BaseUID    = 9000
	MaxUID     = 20000
	NoGroupGID = 65534
	NoBodyUID  = 65534
)

type uidCounter struct {
	uid int16
	mu  sync.Mutex
}

func (u *uidCounter) allocate() (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	maxAttempts := 1000
	for range maxAttempts {
		u.uid++
		if u.uid < BaseUID || u.uid > MaxUID {
			u.uid = BaseUID
		}
		if _, err := user.LookupId(strconv.Itoa(int(u.uid))); err != nil {
			return int(u.uid), nil
		}
	}
	// NoBodyUID is used here to ensure we do not accidentally send back root's UID in a posix system
	return NoBodyUID, errors.New("failed to find unused UID after max attempts")
}

// Global UID counter for process isolation
var globalUIDCounter = &uidCounter{}

// AllocateUID allocates a unique UID for process isolation
func AllocateUID() (int, error) {
	return globalUIDCounter.allocate()
}
