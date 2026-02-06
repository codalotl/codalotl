package simplelogger

import (
	"bytes"
	"fmt"
	"os"
	"sync"
)

var mu sync.Mutex

// Log is a minimal printf-style logger. It appends formatted output to the file
// specified by the CODALOTL_LOG_FILE environment variable.
//
// If CODALOTL_LOG_FILE is unset/empty or the path can't be opened as a file,
// Log is a no-op.
func Log(format string, args ...any) {
	path := os.Getenv("CODALOTL_LOG_FILE")
	if path == "" {
		return
	}

	// Serialize open/write/close to reduce interleaving within a single process.
	mu.Lock()
	defer mu.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	var b bytes.Buffer
	_, _ = fmt.Fprintf(&b, format, args...)
	if b.Len() == 0 || b.Bytes()[b.Len()-1] != '\n' {
		_ = b.WriteByte('\n')
	}
	_, _ = f.Write(b.Bytes())
}
