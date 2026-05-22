package configkit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	configkit "github.com/jaredjakacky/configkit"
)

func TestAsyncObserverDeliversEvent(t *testing.T) {
	received := make(chan configkit.Event, 1)
	async := configkit.NewAsyncObserver(func(ctx context.Context, event configkit.Event) {
		received <- event
	})
	defer closeAsyncObserver(t, async)

	async.Notify(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted, AttemptID: 1})

	event := receiveAsyncEvent(t, received)
	if event.Kind != configkit.EventKindLoadStarted {
		t.Fatalf("event kind = %q, want %q", event.Kind, configkit.EventKindLoadStarted)
	}
	if event.AttemptID != 1 {
		t.Fatalf("attempt id = %d, want 1", event.AttemptID)
	}
	if async.Dropped() != 0 {
		t.Fatalf("dropped = %d, want 0", async.Dropped())
	}
}

func TestAsyncObserverObserverReturnsNotify(t *testing.T) {
	received := make(chan configkit.Event, 1)
	async := configkit.NewAsyncObserver(func(ctx context.Context, event configkit.Event) {
		received <- event
	})
	defer closeAsyncObserver(t, async)

	observer := async.Observer()
	observer(context.Background(), configkit.Event{Kind: configkit.EventKindLoadFailed, AttemptID: 2})

	event := receiveAsyncEvent(t, received)
	if event.Kind != configkit.EventKindLoadFailed {
		t.Fatalf("event kind = %q, want %q", event.Kind, configkit.EventKindLoadFailed)
	}
	if event.AttemptID != 2 {
		t.Fatalf("attempt id = %d, want 2", event.AttemptID)
	}
}

func TestAsyncObserverDeliveryDetachesContextCancellation(t *testing.T) {
	errs := make(chan error, 1)
	async := configkit.NewAsyncObserver(func(ctx context.Context, event configkit.Event) {
		errs <- ctx.Err()
	})
	defer closeAsyncObserver(t, async)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	async.Notify(ctx, configkit.Event{Kind: configkit.EventKindLoadStarted})

	if err := receiveAsyncValue(t, errs); err != nil {
		t.Fatalf("delivered context error = %v, want nil", err)
	}
}

func TestAsyncObserverDropsWhenBufferFull(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	async := configkit.NewAsyncObserver(func(ctx context.Context, event configkit.Event) {
		started <- struct{}{}
		<-release
	}, configkit.WithAsyncObserverBuffer(1))

	async.Notify(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted, AttemptID: 1})
	receiveAsyncSignal(t, started)

	async.Notify(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted, AttemptID: 2})
	async.Notify(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted, AttemptID: 3})

	if got := async.Dropped(); got != 1 {
		close(release)
		closeAsyncObserver(t, async)
		t.Fatalf("dropped = %d, want 1", got)
	}

	close(release)
	closeAsyncObserver(t, async)
}

func TestAsyncObserverNotifyAfterCloseDrops(t *testing.T) {
	async := configkit.NewAsyncObserver(func(ctx context.Context, event configkit.Event) {})
	closeAsyncObserver(t, async)

	async.Notify(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted})

	if got := async.Dropped(); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
}

func TestAsyncObserverCloseDrainsQueuedEvents(t *testing.T) {
	delivered := make(chan uint64, 2)
	async := configkit.NewAsyncObserver(func(ctx context.Context, event configkit.Event) {
		delivered <- event.AttemptID
	}, configkit.WithAsyncObserverBuffer(2))

	async.Notify(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted, AttemptID: 1})
	async.Notify(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted, AttemptID: 2})

	closeAsyncObserver(t, async)

	got := map[uint64]bool{
		receiveAsyncValue(t, delivered): true,
		receiveAsyncValue(t, delivered): true,
	}
	if !got[1] || !got[2] {
		t.Fatalf("delivered attempt ids = %+v, want 1 and 2", got)
	}
}

func TestAsyncObserverCloseReturnsContextErrorWhenObserverBlocked(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	async := configkit.NewAsyncObserver(func(ctx context.Context, event configkit.Event) {
		started <- struct{}{}
		<-release
	})

	async.Notify(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted})
	receiveAsyncSignal(t, started)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := async.Close(ctx); !errors.Is(err, context.Canceled) {
		close(release)
		closeAsyncObserver(t, async)
		t.Fatalf("close error = %v, want context.Canceled", err)
	}

	close(release)
	closeAsyncObserver(t, async)
}

func TestAsyncObserverNilHandling(t *testing.T) {
	var async *configkit.AsyncObserver

	async.Notify(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted})
	if got := async.Dropped(); got != 0 {
		t.Fatalf("nil observer dropped = %d, want 0", got)
	}
	if err := async.Close(context.Background()); err != nil {
		t.Fatalf("nil observer close: %v", err)
	}
}

func TestAsyncObserverNilWrappedObserverDropsNothing(t *testing.T) {
	async := configkit.NewAsyncObserver(nil, configkit.WithAsyncObserverBuffer(1))
	defer closeAsyncObserver(t, async)

	async.Notify(context.Background(), configkit.Event{Kind: configkit.EventKindLoadStarted})

	if got := async.Dropped(); got != 0 {
		t.Fatalf("dropped = %d, want 0", got)
	}
}

func receiveAsyncEvent(t *testing.T, events <-chan configkit.Event) configkit.Event {
	t.Helper()
	return receiveAsyncValue(t, events)
}

func receiveAsyncSignal(t *testing.T, signals <-chan struct{}) {
	t.Helper()
	receiveAsyncValue(t, signals)
}

func receiveAsyncValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		var zero T
		t.Fatal("timed out waiting for async observer")
		return zero
	}
}

func closeAsyncObserver(t *testing.T, async *configkit.AsyncObserver) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := async.Close(ctx); err != nil {
		t.Fatalf("close async observer: %v", err)
	}
}
