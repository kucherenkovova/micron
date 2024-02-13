package micron_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/kucherenkovova/micron"
	"github.com/kucherenkovova/micron/mocks"
)

//go:generate mockgen -source=app_test.go -package=mocks -destination=mocks/irc.go -aux_files=github.com/kucherenkovova/micron=component.go irc
//go:generate mockgen -source=component.go -package=mocks -destination=mocks/component.go Runner,Initializer,Closer

// this interface is used only for mocks generation.
type irc interface { //nolint:unused
	micron.Initializer
	micron.Runner
	micron.Closer
}

type tSuite struct {
	suite.Suite
	ctrl *gomock.Controller
	app  *micron.App
	done chan struct{}
}

func (ts *tSuite) SetupTest() {
	ts.ctrl = gomock.NewController(ts.T())
	ts.app = micron.NewApp()
	ts.done = make(chan struct{})
}

func (ts *tSuite) TearDownTest() {
	ts.ctrl.Finish()
}

func TestAppSuite(t *testing.T) {
	suite.Run(t, new(tSuite))
}

func (ts *tSuite) TestApp_InitSyncOrder() {
	ctx := context.Background()
	first, second := mocks.NewMockInitializer(ts.ctrl), mocks.NewMockInitializer(ts.ctrl)

	gomock.InOrder(
		first.EXPECT().Init(gomock.Any()).Return(nil).Times(1),
		second.EXPECT().Init(gomock.Any()).Return(nil).Times(1),
	)

	ts.app.InitSync(first)
	ts.app.InitSync(second)

	go closeAfter(ts.done, 10*time.Millisecond)

	ts.Require().NoError(ts.app.Start(ctx))
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

	go closeAfter(ts.done, 10*time.Millisecond)

	ts.Require().NoError(ts.app.Start(ctx))
	<-ts.done
}

func (ts *tSuite) TestApp_CloseOrder() {
	ctx := context.Background()
	first, second := mocks.NewMockCloser(ts.ctrl), mocks.NewMockCloser(ts.ctrl)
	gomock.InOrder(
		first.EXPECT().Close(gomock.Any()).Return(nil).Times(1),
		second.EXPECT().Close(gomock.Any()).Return(nil).Times(1),
	)

	ts.app.CloseSync(second)
	ts.app.CloseSync(first)

	go func() {
		<-time.After(10 * time.Millisecond)
		close(ts.done)
	}()
	ts.Require().NoError(ts.app.Start(ctx))
	<-ts.done
}

func (ts *tSuite) TestApp_HandleRunPanic() {
	ctx := context.Background()

	ts.app.Run(micron.RunFunc(func(ctx context.Context) error {
		panic("ooops")
	}))

	err := ts.app.Start(ctx)
	ts.Require().Error(err)
	ts.Require().ErrorIs(err, micron.ErrPanic)
}

func (ts *tSuite) TestApp_HandleInitPanic() {
	ctx := context.Background()

	ts.app.Init(micron.InitFunc(func(ctx context.Context) error {
		panic("ooops")
	}))

	err := ts.app.Start(ctx)

	ts.Require().Error(err)
	ts.Require().ErrorIs(err, micron.ErrPanic)
}

func (ts *tSuite) TestApp_InitPanicWithOnPanicHook() {
	var (
		alertCalledWith any
		alertCalled     = false
		ctx             = context.Background()
	)

	onPanic := func(a any) {
		alertCalled = true
		alertCalledWith = a
	}
	ts.app = micron.NewApp(micron.WithPanicHandler(onPanic))

	ts.app.Init(micron.InitFunc(func(ctx context.Context) error {
		panic("ooops")
	}))

	err := ts.app.Start(ctx)

	ts.Require().Error(err)
	ts.Require().ErrorIs(err, micron.ErrPanic)
	ts.True(alertCalled)
	ts.NotNil(alertCalledWith)
}

func (ts *tSuite) TestApp_RegisterInitializerCloserComponent() {
	ctx := context.Background()
	component := mocks.NewMockirc(ts.ctrl)
	gomock.InOrder(
		component.EXPECT().Init(gomock.Any()).Times(1).Return(nil),
		component.EXPECT().Run(gomock.Any()).Times(1).Return(nil),
		component.EXPECT().Close(gomock.Any()).Times(1).Return(nil),
	)

	ts.app.Register(component)

	go closeAfter(ts.done, 10*time.Millisecond)

	ts.Require().NoError(ts.app.Start(ctx))
	<-ts.done
}

func closeAfter(ch chan struct{}, d time.Duration) {
	<-time.After(d)
	close(ch)
}

func TestAppStopTimeout(t *testing.T) {
	app := micron.NewApp(micron.WithStopTimeout(2 * time.Second))

	closeCalled := false

	app.Close(micron.CloseFunc(func(ctx context.Context) error {
		closeCalled = true
		select {
		case <-ctx.Done():
			require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
			require.ErrorIs(t, context.Cause(ctx), micron.ErrStopTimeout)

			return nil
		case <-time.After(20 * time.Second):
			require.Fail(t, "shouldn't reach here")

			return nil
		}
	}))

	require.NoError(t, app.Start(context.Background()))
	require.True(t, closeCalled)
}
