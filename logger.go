package kinesisvideomanager

type Logger interface {
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
}

var logger Logger = &noopLogger{}

func SetLogger(l Logger) {
	logger = l
}

func GetLogger() Logger {
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
