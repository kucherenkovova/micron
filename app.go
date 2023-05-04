package micron

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type App struct {
	// state stores current App state
	state state

	// application components
	initializers []Initializer
	runners      []Runner
	closers      []Closer

	// callback function used to cancel applicatoin context
	cancel context.CancelFunc

	err error

	// panicHandler stores a callback function
	// that's invoked in case of app panic.
	panicHandler func(any)

	mu      sync.Mutex
	wg      sync.WaitGroup
	errOnce sync.Once
}

func NewApp() *App {
	return &App{}
}

func (a *App) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.state != uninitialized {
		a.mu.Unlock()
		return fmt.Errorf("can't start application in %s state", a.state)
	}

	ctx, a.cancel = context.WithCancel(ctx)
	go a.setupGracefulShutdown(ctx)

	for _, i := range a.initializers {
		if err := a.initialize(ctx, i); err != nil {
			return err
		}
	}
	a.state = initialized

	for _, r := range a.runners {
		a.run(ctx, r)
	}
	a.state = running
	a.mu.Unlock()
	a.wg.Wait()

	return a.err
}

// Stop provides an interface to gracefully stop an application.
// We'll attempt to close all the components resources before exiting.
func (a *App) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	defer a.recover()
	if a.state != running {
		return fmt.Errorf("can't stop application in %s state", a.state)
	}

	a.state = stopping
	// Close components in reverse order
	for i := len(a.closers) - 1; i >= 0; i-- {
		if err := a.closers[i].Close(ctx); err != nil {
			// todo: add warning
			log.Printf("failed to stop component: %v", err)
		}
	}
	log.Println("call cancel")
	a.cancel()
	log.Println("set state as stopped")
	a.state = stopped
	return nil
}

// Init registers Initializer component.
// Initializer components are invoked during application Start process before Run happens.
func (a *App) Init(i Initializer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.initializers = append(a.initializers, i)
}

// Run registers Runner component.
func (a *App) Run(r Runner) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runners = append(a.runners, r)

	if i, ok := r.(Initializer); ok {
		a.initializers = append(a.initializers, i)
	}
	if s, ok := r.(Closer); ok {
		a.closers = append(a.closers, s)
	}
}

// Close registers Closer component.
// Closer components are invoked during application Stop process.
func (a *App) Close(c Closer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closers = append(a.closers, c)
}

// run safely invokes Runner component in a goroutine with panic recovery mechanism.
func (a *App) run(ctx context.Context, r Runner) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer a.recover()

		if err := r.Run(ctx); err != nil {
			a.setError(err)
		}
	}()
}

func (a *App) recover() {
	r := recover()
	if r == nil {
		return
	}

	if a.panicHandler != nil {
		a.panicHandler(r)
	}
	log.Printf("recovered from panic: %v", r)
	a.setError(fmt.Errorf("panic: %v", r))
}

func (a *App) setError(err error) {
	a.errOnce.Do(func() {
		a.err = err
		a.cancel()
	})
}

func (a *App) setupGracefulShutdown(ctx context.Context) {
	defer a.cancel()
	signaled := make(chan os.Signal, 1)
	signal.Notify(signaled, syscall.SIGINT, syscall.SIGTERM)
	select {
	case s := <-signaled:
		log.Printf("exiting %s", s)
		signal.Stop(signaled)
	case <-ctx.Done():
		signal.Stop(signaled)
	}
}

// initialize is a helper function to safely invoke Initializer component with panic recovery mechanism.
func (a *App) initialize(ctx context.Context, i Initializer) error {
	defer a.recover()
	err := i.Init(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialize component: %w", err)
	}
	return nil
}
