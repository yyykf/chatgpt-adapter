//go:build !windows

// Package console sets console's behavior on init
package log

import (
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
)

func init() {
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&logrus.TextFormatter{
		ForceColors:     true,
		TimestampFormat: "2006-01-02 15:03:04",
		CallerPrettyfier: func(frame *runtime.Frame) (string, string) {
			pkg := TrimPkg(path.Dir(frame.Function))
			slice := strings.Split(frame.File, pkg)
			if len(slice) > 1 {
				return path.Dir(frame.Function), TrimLS(slice[1]) + ":" + strconv.Itoa(frame.Line)
			}
			return path.Dir(frame.Function), path.Base(frame.File) + ":" + strconv.Itoa(frame.Line)
		},
	})

	logFile, err := os.OpenFile("logrus.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		logrus.Fatalf("Failed to open log file: %v", err)
	}

	mw := io.MultiWriter(os.Stdout, logFile)

	logrus.SetOutput(mw)
}

func TrimPkg(pkg string) string {
	if pkg == "" {
		return pkg
	}
	slice := strings.Split(pkg, "/")
	length := len(slice)
	if length <= 2 {
		return pkg
	}
	return slice[length-2] + "/" + slice[length-1]
}

func TrimLS(file string) string {
	if file == "" {
		return file
	}
	if strings.HasPrefix(file, "/") {
		return file[1:]
	}
	return file
}
