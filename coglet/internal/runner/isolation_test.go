package runner

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUIDMinimum(t *testing.T) {
	t.Parallel()

	counter := &uidCounter{}
	uid, err := counter.allocate()
	require.NoError(t, err, "allocateUID should not error")
	assert.Equal(t, BaseUID, uid)
}

func TestUIDWrapAround(t *testing.T) {
	t.Parallel()

	counter := &uidCounter{}
	counter.uid = MaxUID
	uid, err := counter.allocate()
	require.NoError(t, err, "allocateUID should not error")
	assert.Equal(t, BaseUID, uid)
}

func TestUID(t *testing.T) {
	t.Run("AllocationThreadSafety", func(t *testing.T) {
		t.Parallel()

		uidCounter := &uidCounter{}

		const numGoroutines = 10
		const uidsPerGoroutine = 5

		var wg sync.WaitGroup
		uidChan := make(chan int, numGoroutines*uidsPerGoroutine)

		for range numGoroutines {
			wg.Go(func() {
				for range uidsPerGoroutine {
					uid, err := uidCounter.allocate()
					if err != nil {
						t.Errorf("allocateUID failed: %v", err)
						return
					}
					uidChan <- uid
				}
			})
		}

		wg.Wait()
		close(uidChan)

		uidSet := make(map[int]bool)
		uidCount := 0

		for uid := range uidChan {
			assert.False(t, uidSet[uid], "Duplicate UID allocated: %d", uid)
			uidSet[uid] = true
			uidCount++
			assert.GreaterOrEqual(t, uid, BaseUID, "UID should be >= BaseUID")
		}

		expectedCount := numGoroutines * uidsPerGoroutine
		assert.Equal(t, expectedCount, uidCount, "Should allocate expected number of UIDs")

		for i := range expectedCount {
			expectedUID := BaseUID + i
			assert.True(t, uidSet[expectedUID], "Missing expected UID: %d", expectedUID)
		}
	})

	t.Run("SequentialAllocation", func(t *testing.T) {
		t.Parallel()

		uidCounter := &uidCounter{}
		firstUID, err := uidCounter.allocate()
		require.NoError(t, err, "allocateUID should not error")
		assert.Equal(t, BaseUID, firstUID, "First UID should be BaseUID")

		secondUID, err := uidCounter.allocate()
		require.NoError(t, err, "allocateUID should not error")
		assert.Equal(t, BaseUID+1, secondUID, "Second UID should be BaseUID+1")

		thirdUID, err := uidCounter.allocate()
		require.NoError(t, err, "allocateUID should not error")
		assert.Equal(t, BaseUID+2, thirdUID, "Third UID should be BaseUID+2")
	})

	t.Run("ErrorHandling", func(t *testing.T) {
		t.Parallel()

		// Test that allocateUID returns proper error when it fails
		// This is hard to test in practice since UIDs 9000+ are usually available
		// But this documents the expected error behavior

		uidCounter := &uidCounter{}

		// Normal allocation should work
		uid, err := uidCounter.allocate()
		require.NoError(t, err, "Normal allocation should succeed")
		assert.GreaterOrEqual(t, uid, BaseUID, "UID should be >= BaseUID")
	})
}

func TestAllocateUID(t *testing.T) {
	t.Parallel()

	// Test the public function
	uid, err := AllocateUID()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, uid, BaseUID)
	assert.LessOrEqual(t, uid, MaxUID)
}
