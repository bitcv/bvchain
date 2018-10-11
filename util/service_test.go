package util

import (
	"testing"
	"time"
)

type testService struct {
	BaseService
}

func (testService) OnReset() error {
	return nil
}

func TestBaseServiceWait(t *testing.T) {
	ts := &testService{}
	ts.BaseService.Init(nil, "TestService", ts)
	ts.Start()

	waitFinished := make(chan struct{})
	go func() {
		ts.WaitForStop()
		waitFinished <- struct{}{}
	}()

	go ts.Stop()

	select {
	case <-waitFinished:
		// all good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected WaitForStop() to finish within 100 ms.")
	}
}

func TestBaseServiceReset(t *testing.T) {
	ts := &testService{}
	ts.BaseService.Init(nil, "TestService", ts)
	ts.Start()

	err := ts.Reset()
	if err == nil {
		t.Error("expected error not occurred")
	}

	ts.Stop()

	err = ts.Reset()
	if err != nil {
		t.Error("unexpected error")
	}

	err = ts.Start()
	if err != nil {
		t.Error("unexpected error")
	}
}

