package log

import (
	"io"
	"os"
	"time"
	"sync/atomic"
	kitlog "github.com/go-kit/kit/log"
)

type Logger interface {
        Debug(msg string, keyvals ...interface{})
        Info(msg string, keyvals ...interface{})
	Warn(msg string, keyvals ...interface{})
        Error(msg string, keyvals ...interface{})

	With(keyvals ...interface{}) Logger
}

var theLevel Level = LEVEL_ERROR

var dftLogger atomic.Value

func init() {
	SetDefaultLogger(New(os.Stderr))
}

func SetLevel(l Level) {
	theLevel = l
}

func SetDefaultLogger(l Logger) {
	if lg, ok := l.(*_Logger); ok {
		l = &_Logger{lg.logger, lg.callerDepth+1}
	}
	dftLogger.Store(l)
}

func Debug(msg string, keyvals ...interface{}) {
	if l, ok := dftLogger.Load().(*_Logger); ok {
		l.Debug(msg, keyvals...)
	}
}

func Info(msg string, keyvals ...interface{}) {
	if l, ok := dftLogger.Load().(*_Logger); ok {
		l.Info(msg, keyvals...)
	}
}

func Warn(msg string, keyvals ...interface{}) {
	if l, ok := dftLogger.Load().(*_Logger); ok {
		l.Warn(msg, keyvals...)
	}
}

func Error(msg string, keyvals ...interface{}) {
	if l, ok := dftLogger.Load().(*_Logger); ok {
		l.Error(msg, keyvals...)
	}
}

type _Logger struct {
	logger kitlog.Logger
	callerDepth int
}

func New(w io.Writer) Logger {
	sw := kitlog.NewSyncWriter(w)
	logger := kitlog.NewLogfmtLogger(sw)
	return &_Logger{logger:logger, callerDepth:5}
}


type Level byte

const (
	LEVEL_ALERT Level = 0
	LEVEL_ERROR	= 1
	LEVEL_WARN	= 2
	LEVEL_NOTICE	= 3
	LEVEL_INFO	= 4
	LEVEL_DEBUG	= 5
	LEVEL_TRACE	= 6
)

func (lvl Level) String() string {
	switch lvl {
	case LEVEL_ALERT:
		return "alert"
	case LEVEL_ERROR:
		return "error"
	case LEVEL_WARN:
		return "warn"
	case LEVEL_NOTICE:
		return "notic"
	case LEVEL_INFO:
		return "info"
	case LEVEL_DEBUG:
		return "debug"
	case LEVEL_TRACE:
		return "trace"
	}
	return "unknown"
}

func timestamp() interface{} {
	return time.Now().Format("060102-150405")
}

func (l *_Logger) doLog(level Level, msg string, keyvals ...interface{}) {
	if level <= theLevel {
		kitlog.With(l.logger, "_t", timestamp(), "_c", kitlog.Caller(l.callerDepth), "_l", level, "_m", msg).Log(keyvals...)
	}
}

func (l *_Logger) Debug(msg string, keyvals ...interface{}) {
	l.doLog(LEVEL_DEBUG, msg, keyvals...)
}

func (l *_Logger) Info(msg string, keyvals ...interface{}) {
	l.doLog(LEVEL_INFO, msg, keyvals...)
}

func (l *_Logger) Warn(msg string, keyvals ...interface{}) {
	l.doLog(LEVEL_WARN, msg, keyvals...)
}

func (l *_Logger) Error(msg string, keyvals ...interface{}) {
	l.doLog(LEVEL_ERROR, msg, keyvals...)
}

func (l *_Logger) With(keyvals ...interface{}) Logger {
	logger := kitlog.With(l.logger, keyvals...)
	return &_Logger{logger:logger, callerDepth:l.callerDepth}
}

