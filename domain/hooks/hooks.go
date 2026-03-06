package hooks

import (
	"context"
	"fmt"
	"sync"
)

// HookFunc is a function called before a read or write operation.
// Return an error to reject the operation.
type HookFunc func(ctx context.Context, entityType, entityID string) error

// Registry manages before-read and before-write hook functions.
// It implements [port.HooksPort].
//
// Hooks are executed in registration order. If any hook returns an error,
// subsequent hooks are skipped and the error is returned. Panicking hooks
// are recovered — the panic is converted to an error and execution continues.
type Registry struct {
	mu          sync.RWMutex
	beforeWrite []namedHook
	beforeRead  []namedHook
}

type namedHook struct {
	name string
	fn   HookFunc
}

// New creates a new empty hooks Registry.
func New() *Registry {
	return &Registry{}
}

// OnBeforeWrite registers a hook that runs before every write.
// Hooks execute in the order they are registered. The name is used
// for error messages and debugging.
func (r *Registry) OnBeforeWrite(name string, fn HookFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beforeWrite = append(r.beforeWrite, namedHook{name: name, fn: fn})
}

// OnBeforeRead registers a hook that runs before every read.
// Hooks execute in the order they are registered.
func (r *Registry) OnBeforeRead(name string, fn HookFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.beforeRead = append(r.beforeRead, namedHook{name: name, fn: fn})
}

// BeforeWrite runs all registered before-write hooks in order.
// Returns the first error encountered. Panics are recovered.
func (r *Registry) BeforeWrite(ctx context.Context, entityType, entityID string) error {
	r.mu.RLock()
	hooks := make([]namedHook, len(r.beforeWrite))
	copy(hooks, r.beforeWrite)
	r.mu.RUnlock()

	return executeHooks(ctx, hooks, entityType, entityID)
}

// BeforeRead runs all registered before-read hooks in order.
// Returns the first error encountered. Panics are recovered.
func (r *Registry) BeforeRead(ctx context.Context, entityType, entityID string) error {
	r.mu.RLock()
	hooks := make([]namedHook, len(r.beforeRead))
	copy(hooks, r.beforeRead)
	r.mu.RUnlock()

	return executeHooks(ctx, hooks, entityType, entityID)
}

// executeHooks runs each hook in order with panic recovery.
func executeHooks(ctx context.Context, hooks []namedHook, entityType, entityID string) error {
	for _, h := range hooks {
		if err := runSafe(ctx, h, entityType, entityID); err != nil {
			return err
		}
	}
	return nil
}

// runSafe executes a single hook with panic recovery.
func runSafe(ctx context.Context, h namedHook, entityType, entityID string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hook %q panicked: %v", h.name, r)
		}
	}()
	return h.fn(ctx, entityType, entityID)
}
