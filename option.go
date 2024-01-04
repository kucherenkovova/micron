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
