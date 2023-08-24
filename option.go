package micron

type Option func(*App)

func WithLogger(log Logger) Option {
	return func(app *App) {
		app.log = log
	}
}
