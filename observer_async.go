package configkit

import (
	"context"
	"sync"
	"sync/atomic"
)

const defaultAsyncObserverBuffer = 64

// AsyncObserver delivers observer events on a background goroutine.
//
// AsyncObserver is an explicit adapter for observers that may block. Notify
// never waits for the wrapped observer. If the buffer is full or the observer
// is closed, the event is dropped and counted by Dropped. Close stops new
// events and waits for queued delivery, but it cannot interrupt the wrapped
// observer if that observer is blocked handling an event.
type AsyncObserver struct {
	observer Observer
	events   chan asyncObserverEvent
	done     chan struct{}

	closeOnce sync.Once
	mu        sync.RWMutex
	closed    bool
	dropped   atomic.Uint64
}

type asyncObserverEvent struct {
	ctx   context.Context
	event Event
}

// AsyncObserverOption configures an AsyncObserver.
type AsyncObserverOption func(*asyncObserverOptions)

type asyncObserverOptions struct {
	buffer int
}

// WithAsyncObserverBuffer configures the number of events an AsyncObserver can
// queue before dropping new events.
//
// The default is 64. Values less than or equal to zero disable buffering.
func WithAsyncObserverBuffer(buffer int) AsyncObserverOption {
	return func(options *asyncObserverOptions) {
		options.buffer = buffer
	}
}

// NewAsyncObserver creates an async adapter for observer.
//
// The returned adapter starts one background goroutine. Call Close during
// shutdown to stop new events and drain queued events. A nil observer is
// accepted but does not receive events.
func NewAsyncObserver(observer Observer, opts ...AsyncObserverOption) *AsyncObserver {
	var options asyncObserverOptions
	options.buffer = defaultAsyncObserverBuffer
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if options.buffer < 0 {
		options.buffer = 0
	}

	async := &AsyncObserver{
		observer: observer,
		events:   make(chan asyncObserverEvent, options.buffer),
		done:     make(chan struct{}),
	}
	go async.run()

	return async
}

// Observer returns the observer function to register with a Manager.
func (o *AsyncObserver) Observer() Observer {
	return o.Notify
}

// Notify queues an event for asynchronous delivery.
//
// Notify is non-blocking. If the queue is full or the observer is closed, the
// event is dropped. Delivery uses a context detached from cancellation so the
// event can still be delivered after the load or apply context has ended.
func (o *AsyncObserver) Notify(ctx context.Context, event Event) {
	if o == nil || o.observer == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.closed {
		o.dropped.Add(1)
		return
	}

	select {
	case o.events <- asyncObserverEvent{ctx: ctx, event: event}:
	default:
		o.dropped.Add(1)
	}
}

// Dropped returns the number of events dropped because the observer was full or
// closed.
func (o *AsyncObserver) Dropped() uint64 {
	if o == nil {
		return 0
	}

	return o.dropped.Load()
}

// Close stops accepting new events and waits for queued events to drain.
//
// If ctx expires before queued events are delivered, Close returns ctx.Err().
// Close cannot preempt the wrapped observer if that observer is blocked while
// handling an event; in that case the background goroutine remains blocked
// until the observer returns.
func (o *AsyncObserver) Close(ctx context.Context) error {
	if o == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	o.closeOnce.Do(func() {
		o.mu.Lock()
		defer o.mu.Unlock()

		o.closed = true
		close(o.events)
	})

	select {
	case <-o.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *AsyncObserver) run() {
	defer close(o.done)

	for event := range o.events {
		o.deliver(event.ctx, event.event)
	}
}

func (o *AsyncObserver) deliver(ctx context.Context, event Event) {
	defer func() {
		_ = recover()
	}()

	o.observer(ctx, event)
}
