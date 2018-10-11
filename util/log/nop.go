package log

type _NopLogger struct {}

func NewNopLogger() Logger {
	return _NopLogger{}
}

func (l _NopLogger) Debug(msg string, keyvals ...interface{}) {
}

func (l _NopLogger) Info(msg string, keyvals ...interface{}) {
}

func (l _NopLogger) Warn(msg string, keyvals ...interface{}) {
}

func (l _NopLogger) Error(msg string, keyvals ...interface{}) {
}

func (l _NopLogger) With(keyvals ...interface{}) Logger {
	return l
}


