package semaphoregroup

import (
	"context"
	"sync"
)

// SemaphoreGroup is a custom type that combines sync.WaitGroup and a semaphore.
type SemaphoreGroup struct {
	waitGroup sync.WaitGroup
	semaphore chan struct{}
}

// NewSemaphoreGroup creates a new SemaphoreGroup with the specified semaphore limit.
func NewSemaphoreGroup(limit int) *SemaphoreGroup {
	return &SemaphoreGroup{
		semaphore: make(chan struct{}, limit),
	}
}

// Add increments the internal WaitGroup counter and acquires a semaphore slot.
func (sg *SemaphoreGroup) Add(ctx context.Context) {
	sg.waitGroup.Add(1)

	// Acquire semaphore slot
	select {
	case <-ctx.Done():
		return
	case sg.semaphore <- struct{}{}:
	}
}

// Done releases a semaphore slot and then decrements the internal WaitGroup
// counter, in that order, so that Wait returning means every slot has been
// released as well. Decrementing first left a window in which Wait had
// returned while the slot was still held.
func (sg *SemaphoreGroup) Done(ctx context.Context) {
	defer sg.waitGroup.Done()

	// Release semaphore slot
	select {
	case <-ctx.Done():
		return
	case <-sg.semaphore:
	}
}

// Wait waits until the internal WaitGroup counter is zero.
func (sg *SemaphoreGroup) Wait() {
	sg.waitGroup.Wait()
}
