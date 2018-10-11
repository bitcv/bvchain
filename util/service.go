package util

import (
	"errors"
	"fmt"
	"sync/atomic"

	"bvchain/util/log"
)

var (
	ErrAlreadyStarted = errors.New("already started")
	ErrAlreadyStopped = errors.New("already stopped")
)

// Service defines a service that can be started, stopped, and reset.
type Service interface {
	// Start the service.
	// If it's already started or stopped, will return an error.
	// If OnStart() returns an error, it's returned by Start()
	Start() error
	OnStart() error

	// Stop the service.
	// If it's already stopped, will return an error.
	// OnStop must never error.
	Stop() error
	OnStop()

	// Reset the service.
	// Panics by default - must be overwritten to enable reset.
	Reset() error
	OnReset() error

	// Return true if the service is running
	IsRunning() bool

	// C4Quit returns a channel, which is closed once service is stopped.
	C4Quit() <-chan struct{}

	// String representation of the service
	String() string

	// SetLogger sets a logger.
	SetLogger(log.Logger)
}

/*
Classical-inheritance-style service declarations. Services can be started, then
stopped, then optionally restarted.

Users can override the OnStart/OnStop methods. In the absence of errors, these
methods are guaranteed to be called at most once. If OnStart returns an error,
service won't be marked as started, so the user can call Start again.

A call to Reset will panic, unless OnReset is overwritten, allowing
OnStart/OnStop to be called again.

The caller must ensure that Start and Stop are not called concurrently.

It is ok to call Stop without calling Start first.

Typical usage:

	type FooService struct {
		BaseService
		// private fields
	}

	func NewFooService() *FooService {
		fs := &FooService{
			// init
		}
		fs.BaseService.Init(log, "FooService", fs)
		return fs
	}

	func (fs *FooService) OnStart() error {
		fs.BaseService.OnStart() // Always call the overridden method.
		// initialize private fields
		// start subroutines, etc.
	}

	func (fs *FooService) OnStop() error {
		fs.BaseService.OnStop() // Always call the overridden method.
		// close/destroy private fields
		// stop subroutines, etc.
	}
*/
type BaseService struct {
	Logger  log.Logger
	name    string
	started uint32 // atomic
	stopped uint32 // atomic
	c4quit  chan struct{}

	// The "subclass" of BaseService
	impl Service
}

var _ Service = (*BaseService)(nil)


func (bs *BaseService) Init(logger log.Logger, name string, impl Service) {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	bs.Logger = logger
	bs.name = name
	bs.c4quit = make(chan struct{})
	bs.impl = impl
}

// SetLogger implements Service by setting a logger.
func (bs *BaseService) SetLogger(l log.Logger) {
	bs.Logger = l
}

var myFmt = func(format string, a ...interface{}) string {
        if len(a) == 0 {
                return format
        }
        return fmt.Sprintf(format, a...)
}

// Start implements Service by calling OnStart (if defined). An error will be
// returned if the service is already running or stopped. Not to start the
// stopped service, you need to call Reset.
func (bs *BaseService) Start() error {
	if atomic.CompareAndSwapUint32(&bs.started, 0, 1) {
		if atomic.LoadUint32(&bs.stopped) == 1 {
			bs.Logger.Error(myFmt("Not starting %v -- already stopped", bs.name), "impl", bs.impl)
			return ErrAlreadyStopped
		}
		bs.Logger.Info(myFmt("Starting %v", bs.name), "impl", bs.impl)
		err := bs.impl.OnStart()
		if err != nil {
			// revert flag
			atomic.StoreUint32(&bs.started, 0)
			return err
		}
		return nil
	}
	bs.Logger.Debug(myFmt("Not starting %v -- already started", bs.name), "impl", bs.impl)
	return ErrAlreadyStarted
}

// OnStart implements Service by doing nothing.
// NOTE: Do not put anything in here,
// that way users don't need to call BaseService.OnStart()
func (bs *BaseService) OnStart() error { return nil }

// Stop implements Service by calling OnStop (if defined) and closing quit
// channel. An error will be returned if the service is already stopped.
func (bs *BaseService) Stop() error {
	if atomic.CompareAndSwapUint32(&bs.stopped, 0, 1) {
		bs.Logger.Info(myFmt("Stopping %v", bs.name), "impl", bs.impl)
		bs.impl.OnStop()
		close(bs.c4quit)
		return nil
	}
	bs.Logger.Debug(myFmt("Stopping %v (ignoring: already stopped)", bs.name), "impl", bs.impl)
	return ErrAlreadyStopped
}

// OnStop implements Service by doing nothing.
// NOTE: Do not put anything in here,
// that way users don't need to call BaseService.OnStop()
func (bs *BaseService) OnStop() {}

// Reset implements Service by calling OnReset callback (if defined). An error
// will be returned if the service is running.
func (bs *BaseService) Reset() error {
	if !atomic.CompareAndSwapUint32(&bs.stopped, 1, 0) {
		bs.Logger.Debug(myFmt("Can't reset %v. Not stopped", bs.name), "impl", bs.impl)
		return fmt.Errorf("can't reset running %s", bs.name)
	}

	// whether or not we've started, we can reset
	atomic.CompareAndSwapUint32(&bs.started, 1, 0)

	bs.c4quit = make(chan struct{})
	return bs.impl.OnReset()
}

// OnReset implements Service by panicking.
func (bs *BaseService) OnReset() error {
	panic("The service cannot be reset")
	return nil
}

// IsRunning implements Service by returning true or false depending on the
// service's state.
func (bs *BaseService) IsRunning() bool {
	return atomic.LoadUint32(&bs.started) == 1 && atomic.LoadUint32(&bs.stopped) == 0
}

// WaitForStop blocks until the service is stopped.
func (bs *BaseService) WaitForStop() {
	<-bs.c4quit
}

// String implements Servce by returning a string representation of the service.
func (bs *BaseService) String() string {
	return bs.name
}

// C4Quit Implements Service by returning a quit channel.
func (bs *BaseService) C4Quit() <-chan struct{} {
	return bs.c4quit
}
