package micron

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/kucherenkovova/micron/logger"
)

type Logger interface {
	Debug(string)
	Info(string)
	Warn(string)
	Error(string)
}

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
	log     Logger
}

type Option func(*App)

func NewApp(opts ...Option) *App {
	app := &App{
		log: logger.New(logger.DEBUG),
	}

	for _, option := range opts {
		option(app)
	}

	return app
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
			a.log.Warn(fmt.Sprintf("failed to stop component: %v", err))
		}
	}
	a.log.Debug("call cancel func")
	a.cancel()
	a.log.Debug("set state as stopped")

	a.state = stopped

	return nil
}

// Register is a shorthand function to register a component that
// implements multiple micron lifecycle hooks.
func (a *App) Register(component any) *App {
	if component == nil {
		a.log.Warn("nil component registered")

		return a
	}

	knownComponentType := false

	if i, ok := component.(Initializer); ok {
		a.initializers = append(a.initializers, i)
		knownComponentType = true
	}

	if r, ok := component.(Runner); ok {
		a.runners = append(a.runners, r)
		knownComponentType = true
	}

	if s, ok := component.(Closer); ok {
		a.closers = append(a.closers, s)
		knownComponentType = true
	}

	if !knownComponentType {
		a.log.Warn(fmt.Sprintf("unknown component registered: %v", component))
	}

	return a
}

// Init registers Initializer component.
// Initializer components are invoked during application Start process before Run happens.
func (a *App) Init(i Initializer) *App {
	a.mu.Lock()
	a.initializers = append(a.initializers, i)
	a.mu.Unlock()

	return a
}

// Run registers Runner component.
func (a *App) Run(r Runner) *App {
	a.mu.Lock()
	a.runners = append(a.runners, r)
	a.mu.Unlock()

	return a
}

// Close registers Closer component.
// Closer components are invoked during application Stop process.
func (a *App) Close(c Closer) *App {
	a.mu.Lock()
	a.closers = append(a.closers, c)
	a.mu.Unlock()

	return a
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

	a.log.Error(fmt.Sprintf("recovered from panic: %v", r))
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
		a.log.Debug(fmt.Sprintf("exiting %s", s))
		signal.Stop(signaled)
	case <-ctx.Done():
		signal.Stop(signaled)
	}
}

// initialize is a helper function to safely invoke Initializer component with panic recovery mechanism.
func (a *App) initialize(ctx context.Context, i Initializer) error {
	defer a.recover()

	if err := i.Init(ctx); err != nil {
		return fmt.Errorf("failed to initialize component: %w", err)
	}

	return nil
}
