package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Короткий журнал в %APPDATA%\T31K Binder\binder.log — чтобы разбираться,
// если у кого-то что-то не работает. Больше 256 КБ не растёт.
var (
	logMu   sync.Mutex
	logFile *os.File
)

func openLog() {
	_ = os.MkdirAll(configDir(), 0o755)
	path := filepath.Join(configDir(), "binder.log")
	flag := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if st, err := os.Stat(path); err == nil && st.Size() > 256*1024 {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err == nil {
		logFile = f
	}
}

func logf(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile == nil {
		return
	}
	fmt.Fprintf(logFile, "%s [%d] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), os.Getpid(), fmt.Sprintf(format, args...))
}
