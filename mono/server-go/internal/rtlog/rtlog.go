// Package rtlog sets up dual-write logging (stdout + file in RTVORTEX_HOME/temp/).
//
// Log file naming matches the C++ engine convention:
//
//	rtgoserver_<hostname>_YYYYMMDD.log
//
// The file is opened in append mode so multiple runs on the same day share
// a single log. The returned cleanup function should be deferred.
package rtlog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/rtenv"
)

// Setup creates a log file under env.TempDir and configures the default
// logger to write to both stdout and the file.
//
// Returns a cleanup function that closes the log file.
func Setup(env *rtenv.Env) (cleanup func(), err error) {
	filename := fmt.Sprintf("rtgoserver_%s_%s.log",
		env.Hostname,
		time.Now().Format("20060102"),
	)
	path := filepath.Join(env.TempDir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}

	// Dual-write: stdout + file.
	multi := io.MultiWriter(os.Stdout, f)
	log.SetOutput(multi)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetPrefix(fmt.Sprintf("[%s] ", env.Hostname))

	log.Printf("[INFO] Log file: %s", path)

	return func() {
		log.SetOutput(os.Stdout) // restore before close
		_ = f.Close()
	}, nil
}
