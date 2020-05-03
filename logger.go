package kinesisvideomanager

type LoggerIF interface {
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
}

var logger LoggerIF = &noopLogger{}

func SetLogger(l LoggerIF) {
	logger = l
}

func Logger() LoggerIF {
	return logger
}

type noopLogger struct {
}

func (n *noopLogger) Debug(args ...interface{}) {
}

func (n *noopLogger) Debugf(format string, args ...interface{}) {
}

func (n *noopLogger) Info(args ...interface{}) {
}

func (n *noopLogger) Infof(format string, args ...interface{}) {
}

func (n *noopLogger) Warn(args ...interface{}) {
}

func (n *noopLogger) Warnf(format string, args ...interface{}) {
}

func (n *noopLogger) Error(args ...interface{}) {
}

func (n *noopLogger) Errorf(format string, args ...interface{}) {
}
