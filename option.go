package micron

import (
	"time"
)

type Option func(*App)

func WithLogger(log Logger) Option {
	return func(app *App) {
		app.log = log
	}
}

func WithStopTimeout(timeout time.Duration) Option {
	return func(app *App) {
		app.stopTimeout = timeout
	}
}

func WithPanicHandler(handlePanicFn func(any)) Option {
	return func(app *App) {
		app.handlePanicFn = handlePanicFn
	}
}

func WithGracefulShutdown() Option {
	return func(app *App) {
		app.enableGracefulShutdown = true
	}
}

func WithConcurrency(concurrency int) Option {
	return func(app *App) {
		app.concurrency = concurrency
	}
}

type RegisterOption func(*ComponentConfig)

func WithInitSync() RegisterOption {
	return func(config *ComponentConfig) {
		config.initSync = true
	}
}

func WithCloseSync() RegisterOption {
	return func(config *ComponentConfig) {
		config.closeSync = true
	}
}
