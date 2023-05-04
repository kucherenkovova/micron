package micron

// OnPanic registers a callback function called in case of panic.
// Usually used to notify an external system (slack/email/sentry/etc) about unhandled issue in the running app.
// App instance can have only one panic callback. Every OnPanic invocation overwrites previously registered handler.
func (a *App) OnPanic(f func(any)) {
	a.panicHandler = f
}
