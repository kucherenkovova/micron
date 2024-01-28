package micron

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/kucherenkovova/safegroup"
	"golang.org/x/sync/semaphore"
)

const (
	defaultStopTimeout = 20 * time.Second
)

var (
	ErrPanic       = errors.New("panic occurred")
	ErrStopTimeout = errors.New("stop timeout exceeded")
)

type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type App struct {
	// application components
	initializers     []Initializer
	syncInitializers []Initializer
	runners          []Runner
	closers          []Closer
	syncClosers      []Closer

	// concurrency is the number of goroutines to use for initialization and closing.
	// Default value is runtime.NumCPU().
	concurrency int

	// handlePanicFn stores a callback function
	// that's invoked in case of app panic.
	handlePanicFn func(any)

	// enableGracefulShutdown is a flag that indicates whether to
	// stop the application on syscall.SIGINT and syscall.SIGTERM signals.
	enableGracefulShutdown bool

	// stopTimeout is the maximum amount of time to wait for the application to stop.
	stopTimeout time.Duration

	mu  sync.Mutex
	log Logger
}

func NewApp(opts ...Option) *App {
	app := &App{
		concurrency: runtime.NumCPU(),
		stopTimeout: defaultStopTimeout,
	}

	for _, option := range opts {
		option(app)
	}

	if app.log == nil {
		app.log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	}

	return app
}

func (a *App) Start(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			a.handlePanic(r)
			err = fmt.Errorf("micron app: %w: %v", ErrPanic, r)
		}
	}()

	if a.enableGracefulShutdown {
		var cancel context.CancelFunc
		ctx, cancel = signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
	}

	if err := a.initialize(ctx); err != nil {
		return err
	}

	if err := a.run(ctx); err != nil {
		return err
	}

	return a.close(context.Background())
}

func (a *App) initialize(ctx context.Context) error {
	// todo: think about closing all the initialized components if one of them fails

	for i := range a.syncInitializers {
		if err := a.syncInitializers[i].Init(ctx); err != nil {
			return err
		}
	}

	if len(a.initializers) == 0 {
		return nil
	}

	g, ctx := safegroup.WithContext(ctx)
	g.SetLimit(a.concurrency)

	for i := range a.initializers {
		i := i
		g.Go(func() error {
			return a.initializers[i].Init(ctx)
		})
	}

	err := g.Wait()
	if err == nil {
		return nil
	}

	if errors.Is(err, safegroup.ErrPanic) {
		err = fmt.Errorf("initialize %w: %w", ErrPanic, err)
		a.handlePanic(err)
	}

	return err
}

func (a *App) run(ctx context.Context) error {
	g, ctx := safegroup.WithContext(ctx)
	for _, r := range a.runners {
		r := r
		g.Go(func() error {
			return r.Run(ctx)
		})
	}

	err := g.Wait()
	if err == nil {
		return nil
	}
	if errors.Is(err, safegroup.ErrPanic) {
		err = fmt.Errorf("run %w: %w", ErrPanic, err)
		a.handlePanic(err)
	}

	return err
}

func (a *App) close(ctx context.Context) (err error) {
	if a.stopTimeout > 0 {
		wrappedCtx, cancel := context.WithTimeoutCause(ctx, a.stopTimeout, ErrStopTimeout)
		ctx = wrappedCtx
		defer cancel()
	}

	// Close components in reverse order
	for i := len(a.syncClosers) - 1; i >= 0; i-- {
		if e := a.syncClosers[i].Close(ctx); e != nil {
			a.log.Warn(fmt.Sprintf("failed to close component: %v", e))
			err = e
		}
	}
	if len(a.closers) == 0 {
		return nil
	}

	// Attempt to close the components concurrently, but limit the number of concurrent goroutines.
	// We use a semaphore, not safegroup here because we want other closers to be called even if one of them fails.
	sem := semaphore.NewWeighted(int64(a.concurrency))
	for i := range a.closers {
		if err := sem.Acquire(ctx, 1); err != nil {
			a.log.Warn("failed to acquire semaphore: %v", err)
		}

		go func(i int) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("close: %w: %v", ErrPanic, r)
				}
			}()
			defer sem.Release(1)

			if e := a.closers[i].Close(ctx); e != nil {
				a.log.Warn(fmt.Sprintf("failed to close component: %v", e))
				err = e
			}
		}(i)
	}
	// wait for all the components to close
	if err := sem.Acquire(ctx, int64(a.concurrency)); err != nil {
		a.log.Warn("failed to acquire semaphore: %v", err)
	}

	switch {
	case err == nil:
		return nil
	case errors.Is(err, safegroup.ErrPanic):
		err = fmt.Errorf("close: %w: %w", ErrPanic, err)
		a.handlePanic(err)
	case errors.Is(err, context.Canceled):
		err = fmt.Errorf("close: %w: %w", err, context.Cause(ctx))
	}

	return err
}

type ComponentConfig struct {
	initSync  bool
	closeSync bool
}

// Register is a shorthand function to register a component that
// implements multiple micron lifecycle hooks.
func (a *App) Register(component any, opts ...RegisterOption) *App {
	if component == nil {
		a.log.Warn("nil component registered, this is probably a misconfiguration of your application")

		return a
	}

	config := &ComponentConfig{}
	for _, opt := range opts {
		opt(config)
	}

	knownComponentType := false

	if i, ok := component.(Initializer); ok {
		if config.initSync {
			a.InitSync(i)
		} else {
			a.Init(i)
		}
		knownComponentType = true
	}

	if r, ok := component.(Runner); ok {
		a.runners = append(a.runners, r)
		knownComponentType = true
	}

	if c, ok := component.(Closer); ok {
		if config.closeSync {
			a.CloseSync(c)
		} else {
			a.Close(c)
		}
		knownComponentType = true
	}

	if !knownComponentType {
		a.log.Warn(fmt.Sprintf("unknown component registered: %v", component))
	}

	return a
}

// Init registers Initializer component.
// Initializer components registered using this method will be run concurrently during the application Start process.
func (a *App) Init(i Initializer) *App {
	a.mu.Lock()
	a.initializers = append(a.initializers, i)
	a.mu.Unlock()

	return a
}

// InitSync registers Initializer component.
// Initializer components registered using this method will be run sequentially during the application Start process.
func (a *App) InitSync(i Initializer) *App {
	a.mu.Lock()
	a.syncInitializers = append(a.syncInitializers, i)
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
// Closer components registered using this method will be run concurrently during the application Stop process.
func (a *App) Close(c Closer) *App {
	a.mu.Lock()
	a.closers = append(a.closers, c)
	a.mu.Unlock()

	return a
}

// CloseSync registers Closer component.
// Closer components registered using this method will be run sequentially during the application Stop process.
func (a *App) CloseSync(c Closer) *App {
	a.mu.Lock()
	a.syncClosers = append(a.syncClosers, c)
	a.mu.Unlock()

	return a
}

func (a *App) handlePanic(r any) {
	if a.handlePanicFn != nil {
		a.handlePanicFn(r)
	}
}
