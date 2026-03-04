// Package rtenv resolves the RTVortex runtime environment.
//
// Environment layout (matches the C++ engine layout under RTVORTEX_HOME):
//
//	RTVORTEX_HOME/
//	├── bin/         executables (rtvortex, RTVortexGo)
//	├── config/      rtserverprops.xml, vcsplatforms.xml, default.yml
//	├── data/        persistent index data
//	├── temp/        logs + temporary working files (RT_TEMP)
//	├── models/      ONNX embeddings
//
// Resolution order for RTVORTEX_HOME:
//  1. RTVORTEX_HOME env var
//  2. RT_HOME env var (shared with launcher scripts)
//  3. Executable's parent directory (or grandparent if in bin/)
//  4. Current working directory
package rtenv

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Env holds the resolved RTVortex directory layout.
type Env struct {
	Home      string // RTVORTEX_HOME root
	ConfigDir string // RTVORTEX_HOME/config
	DataDir   string // RTVORTEX_HOME/data
	TempDir   string // RTVORTEX_HOME/temp
	ModelsDir string // RTVORTEX_HOME/models
	Hostname  string // machine hostname
}

// Resolve discovers RTVORTEX_HOME and builds the directory layout.
// It creates logs/ and tmp/ if they do not exist.
func Resolve() (*Env, error) {
	home := resolveHome()

	env := &Env{
		Home:      home,
		ConfigDir: filepath.Join(home, "config"),
		DataDir:   filepath.Join(home, "data"),
		TempDir:   filepath.Join(home, "temp"),
		ModelsDir: filepath.Join(home, "models"),
		Hostname:  hostname(),
	}

	// Create directories that may not exist yet.
	for _, dir := range []string{env.DataDir, env.TempDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	return env, nil
}

// resolveHome determines the RTVORTEX_HOME directory.
func resolveHome() string {
	// 1. RTVORTEX_HOME env var (canonical).
	if v := os.Getenv("RTVORTEX_HOME"); v != "" {
		return v
	}

	// 2. RT_HOME env var (shared with launcher scripts).
	if v := os.Getenv("RT_HOME"); v != "" {
		return v
	}

	// 3. Executable's parent (or grandparent if binary is in bin/).
	if exe, err := os.Executable(); err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		dir := filepath.Dir(exe)
		if filepath.Base(dir) == "bin" {
			return filepath.Dir(dir)
		}
		return dir
	}

	// 4. Current working directory.
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}

	return "."
}

// hostname returns the machine hostname, or "unknown".
func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "unknown"
}

// BinaryName returns the canonical binary name for the current OS.
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "RTVortexGo.exe"
	}
	return "RTVortexGo"
}

// BinarySuffix returns the OS-arch suffix for cross-compiled binaries.
func BinarySuffix() string {
	suffix := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		suffix += ".exe"
	}
	return suffix
}
