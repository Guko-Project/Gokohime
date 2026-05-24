package log

import (
	"fmt"
	"os"
	"strings"
	"time"

	charm "github.com/charmbracelet/log"
	"github.com/sirupsen/logrus"
)

const maxMessageLength = 200

var logger *charm.Logger

func init() {
	logger = charm.NewWithOptions(os.Stderr, charm.Options{
		ReportTimestamp: true,
		TimeFormat:      time.DateTime,
		Level:          charm.InfoLevel,
	})
}

func Init(debug bool) {
	if debug {
		logger.SetLevel(charm.DebugLevel)
	}
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetOutput(os.Stderr)
	logrus.AddHook(&forwardHook{})
	logrus.SetFormatter(&nullFormatter{})
}

func Info(args ...any)                    { logger.Info(fmt.Sprint(args...)) }
func Infof(format string, args ...any)    { logger.Infof(format, args...) }
func Warn(args ...any)                    { logger.Warn(fmt.Sprint(args...)) }
func Warnf(format string, args ...any)    { logger.Warnf(format, args...) }
func Debug(args ...any)                   { logger.Debug(fmt.Sprint(args...)) }
func Debugf(format string, args ...any)   { logger.Debugf(format, args...) }
func Error(args ...any)                   { logger.Error(fmt.Sprint(args...)) }
func Errorf(format string, args ...any)   { logger.Errorf(format, args...) }
func Fatal(args ...any)                   { logger.Fatal(fmt.Sprint(args...)) }
func Fatalf(format string, args ...any)   { logger.Fatalf(format, args...) }

// forwardHook intercepts logrus output (from ZeroBot) and forwards to charm logger.
type forwardHook struct{}

func (h *forwardHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *forwardHook) Fire(entry *logrus.Entry) error {
	msg := truncate(entry.Message)

	switch entry.Level {
	case logrus.DebugLevel, logrus.TraceLevel:
		logger.Debug(msg)
	case logrus.InfoLevel:
		logger.Info(msg)
	case logrus.WarnLevel:
		logger.Warn(msg)
	case logrus.ErrorLevel:
		logger.Error(msg)
	case logrus.FatalLevel, logrus.PanicLevel:
		logger.Error(msg)
	}
	return nil
}

// nullFormatter prevents logrus from writing its own output since we forward via hook.
type nullFormatter struct{}

func (f *nullFormatter) Format(_ *logrus.Entry) ([]byte, error) {
	return nil, nil
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= maxMessageLength {
		return s
	}
	return string(runes[:maxMessageLength]) + "…(truncated)"
}
