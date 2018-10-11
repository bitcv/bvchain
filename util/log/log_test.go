package log

import (
	"testing"
	"os"
)

func TestLog(t *testing.T) {
	Debug("hello, world!", "num", 123)
	Error("hello, world!", "num", 123)

	logger := New(os.Stderr)
	logger = logger.With("module", "test")
	SetDefaultLogger(logger)

	SetLevel(LEVEL_TRACE)
	Debug("hello, world!", "num", 123)
	logger.Info("oh, my love, my darling", "who", "her")

}

