// Copyright 2020 SEQSENSE, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
