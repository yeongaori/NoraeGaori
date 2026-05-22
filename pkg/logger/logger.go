package logger

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

var (
	debugMode bool

	
	errorLogFile *os.File
	errorLogMu   sync.Mutex

	
	

	
	infoBadge = "\033[48;5;10m\x1b[37m INFO \033[0m"

	
	warnBadge = "\033[48;5;3m\x1b[37m WARN \033[0m"

	
	errorBadge = "\033[48;5;9m\x1b[37m ERRO \033[0m"

	
	debugBadge = "\033[48;5;202m\x1b[37m DEBG \033[0m"

	
	termBadge = "\033[48;5;6m\x1b[37m TERM \033[0m"
)

func Initialize(debug bool) {
	debugMode = debug
	log.SetOutput(os.Stdout)
	log.SetFlags(0) 

	
	var err error
	errorLogFile, err = os.OpenFile("error.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Warning: Failed to open error.log: %v\n", err)
	}
}

func Close() {
	errorLogMu.Lock()
	defer errorLogMu.Unlock()
	if errorLogFile != nil {
		errorLogFile.Close()
	}
}

func logToFile(level, message string) {
	errorLogMu.Lock()
	defer errorLogMu.Unlock()

	if errorLogFile == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, message)
	errorLogFile.WriteString(line)
}

func Info(message string) {
	fmt.Printf("%s %s\n", infoBadge, message)
}

func Warn(message string) {
	fmt.Printf("%s %s\n", warnBadge, message)
	logToFile("WARN", message)
}

func Error(message string) {
	fmt.Printf("%s %s\n", errorBadge, message)
	logToFile("ERRO", message)
}

func Debug(message string) {
	if debugMode {
		fmt.Printf("%s %s\n", debugBadge, message)
	}
}

func Term(message string) {
	fmt.Printf("%s %s\n", termBadge, message)
}

func Infof(format string, args ...interface{}) {
	Info(fmt.Sprintf(format, args...))
}

func Warnf(format string, args ...interface{}) {
	Warn(fmt.Sprintf(format, args...))
}

func Errorf(format string, args ...interface{}) {
	Error(fmt.Sprintf(format, args...))
}

func Debugf(format string, args ...interface{}) {
	Debug(fmt.Sprintf(format, args...))
}

func IsDebug() bool {
	return debugMode
}
