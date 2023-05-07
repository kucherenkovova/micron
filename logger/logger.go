package logger

import (
	"io"
	"log"
	"os"
)

type Level int8

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	}
	return "DEBUG"
}

type Logger struct {
	inner [4]*log.Logger
}

func New(level Level) *Logger {
	l := &Logger{}

	for lvl := ERROR; lvl >= DEBUG; lvl-- {
		if level > lvl {
			l.inner[lvl] = log.New(io.Discard, lvl.String()+" ", log.LstdFlags)
			continue
		}

		l.inner[lvl] = log.New(os.Stderr, lvl.String()+" ", log.LstdFlags)
	}

	return l
}

func (l *Logger) Debug(msg string) {
	l.inner[DEBUG].Println(msg)
}

func (l *Logger) Info(msg string) {
	l.inner[INFO].Println(msg)
}

func (l *Logger) Warn(msg string) {
	l.inner[WARN].Println(msg)
}

func (l *Logger) Error(msg string) {
	l.inner[ERROR].Println(msg)
}
