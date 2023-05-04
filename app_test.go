package micron

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/suite"
	"go.uber.org/goleak"

	"github.com/kucherenkovova/micron/mocks"
)

//go:generate mockgen -source=app_test.go -package=mocks -destination=mocks/irc.go -aux_files=github.com/kucherenkovova/micron=component.go irc
//go:generate mockgen -source=component.go -package=mocks -destination=mocks/component.go Runner,Initializer,Closer

// this interface is used only for mocks generation.
type irc interface { //nolint:unused
	Initializer
	Runner
	Closer
}

type tSuite struct {
	suite.Suite
	ctrl *gomock.Controller
	app  *App
	done chan struct{}
}

func (ts *tSuite) SetupTest() {
	ts.ctrl = gomock.NewController(ts.T())
	ts.app = NewApp()
	ts.done = make(chan struct{})
}

func (ts *tSuite) TearDownTest() {
	ts.ctrl.Finish()
}

func TestAppSuite(t *testing.T) {
	suite.Run(t, new(tSuite))
}

func (ts *tSuite) TestApp_InitOrder() {
	ctx := context.Background()
	first, second := mocks.NewMockInitializer(ts.ctrl), mocks.NewMockInitializer(ts.ctrl)

	gomock.InOrder(
		first.EXPECT().Init(gomock.Any()).Return(nil).Times(1),
		second.EXPECT().Init(gomock.Any()).Return(nil).Times(1),
	)

	ts.app.Init(first)
	ts.app.Init(second)

	go func() {
		<-time.After(10 * time.Millisecond)
		ts.NoError(ts.app.Stop(context.Background()))
		close(ts.done)
	}()

	ts.NoError(ts.app.Start(ctx))
	<-ts.done
}

func (ts *tSuite) TestApp_InitAndRunOrder() {
	ctx := context.Background()
	initme, runme := mocks.NewMockInitializer(ts.ctrl), mocks.NewMockRunner(ts.ctrl)

	gomock.InOrder(
		initme.EXPECT().Init(gomock.Any()).Return(nil).Times(1),
		runme.EXPECT().Run(gomock.Any()).Return(nil).Times(1),
	)

	ts.app.Init(initme)
	ts.app.Run(runme)

	go func() {
		<-time.After(10 * time.Millisecond)
		ts.NoError(ts.app.Stop(context.Background()))
		close(ts.done)
	}()
	ts.NoError(ts.app.Start(ctx))
	<-ts.done
}

func (ts *tSuite) TestApp_CloseOrder() {
	ctx := context.Background()
	first, second := mocks.NewMockCloser(ts.ctrl), mocks.NewMockCloser(ts.ctrl)
	gomock.InOrder(
		first.EXPECT().Close(gomock.Any()).Return(nil).Times(1),
		second.EXPECT().Close(gomock.Any()).Return(nil).Times(1),
	)

	ts.app.Close(second)
	ts.app.Close(first)

	go func() {
		<-time.After(10 * time.Millisecond)
		ts.NoError(ts.app.Stop(context.Background()))
		close(ts.done)
	}()
	ts.NoError(ts.app.Start(ctx))
	<-ts.done
}

func (ts *tSuite) TestApp_NoLeakedGoroutines() {
	defer goleak.VerifyNone(ts.T())

	ctx := context.Background()

	ts.app.Init(InitFunc(func(context.Context) error {
		return nil
	}))
	ts.app.Run(RunFunc(func(context.Context) error {
		return nil
	}))
	ts.app.Close(CloseFunc(func(context.Context) error {
		return nil
	}))
	ts.app.OnPanic(func(any) {})

	go func() {
		<-time.After(10 * time.Millisecond)
		ts.NoError(ts.app.Stop(context.Background()))
		close(ts.done)
	}()
	ts.NoError(ts.app.Start(ctx))
	<-ts.done
}

func (ts *tSuite) TestApp_HandleRunPanic() {
	ctx := context.Background()

	ts.app.Run(RunFunc(func(ctx context.Context) error {
		panic("ooops")
	}))

	err := ts.app.Start(ctx)
	ts.Error(err)
	ts.ErrorContains(err, "panic: ooops")
}

func (ts *tSuite) TestApp_HandleInitPanic() {
	ctx := context.Background()

	ts.app.Init(InitFunc(func(ctx context.Context) error {
		panic("ooops")
	}))

	err := ts.app.Start(ctx)

	ts.Error(err)
	ts.ErrorContains(err, "panic: ooops")
}

func (ts *tSuite) TestApp_InitPanicWithOnPanicHook() {
	var (
		alertCalledWith any
		alertCalled     = false
		ctx             = context.Background()
	)

	ts.app.OnPanic(func(a any) {
		alertCalled = true
		alertCalledWith = a
	})
	ts.app.Init(InitFunc(func(ctx context.Context) error {
		panic("ooops")
	}))

	err := ts.app.Start(ctx)

	ts.Error(err)
	ts.ErrorContains(err, "panic: ooops")
	ts.True(alertCalled)
	ts.NotNil(alertCalledWith)
}

func (ts *tSuite) TestApp_RunInitializerCloserComponent() {
	ctx := context.Background()
	component := mocks.NewMockirc(ts.ctrl)
	gomock.InOrder(
		component.EXPECT().Init(gomock.Any()).Times(1).Return(nil),
		component.EXPECT().Run(gomock.Any()).Times(1).Return(nil),
		component.EXPECT().Close(gomock.Any()).Times(1).Return(nil),
	)

	ts.app.Run(component)

	go func() {
		<-time.After(10 * time.Millisecond)
		ts.NoError(ts.app.Stop(context.Background()))
		close(ts.done)
	}()

	ts.NoError(ts.app.Start(ctx))
	<-ts.done
}
