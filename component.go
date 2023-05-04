package micron

import "context"

type Initializer interface {
	Init(ctx context.Context) error
}

type InitFunc func(ctx context.Context) error

func (f InitFunc) Init(ctx context.Context) error {
	return f(ctx)
}

type Runner interface {
	Run(ctx context.Context) error
}

type RunFunc func(ctx context.Context) error

func (rf RunFunc) Run(ctx context.Context) error {
	return rf(ctx)
}

type Closer interface {
	Close(ctx context.Context) error
}

type CloseFunc func(ctx context.Context) error

func (cf CloseFunc) Close(ctx context.Context) error {
	return cf(ctx)
}
