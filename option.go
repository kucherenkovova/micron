package micron

import "github.com/kucherenkovova/micron/logger"

type Option func(*App)

func WithLogLevel(lvl logger.Level) Option {
	return func(app *App) {
		app.log = logger.New(lvl)
	}
}
