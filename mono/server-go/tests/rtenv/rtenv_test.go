package rtenv_test

import (
	"os"
	"runtime"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/rtenv"
)

func TestResolve_RTVORTEX_HOME(t *testing.T) {
	// Set RTVORTEX_HOME and verify it's picked up.
	tmpDir := t.TempDir()
	t.Setenv("RTVORTEX_HOME", tmpDir)

	env, err := rtenv.Resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Home != tmpDir {
		t.Errorf("expected Home=%s, got %s", tmpDir, env.Home)
	}
}

func TestResolve_RT_HOME_Fallback(t *testing.T) {
	// Unset RTVORTEX_HOME, set RT_HOME.
	tmpDir := t.TempDir()
	t.Setenv("RTVORTEX_HOME", "")
	t.Setenv("RT_HOME", tmpDir)

	env, err := rtenv.Resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Home != tmpDir {
		t.Errorf("expected Home=%s, got %s", tmpDir, env.Home)
	}
}

func TestResolve_CreatesSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("RTVORTEX_HOME", tmpDir)

	env, err := rtenv.Resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// data/ and temp/ should be created by Resolve().
	for _, dir := range []string{env.DataDir, env.TempDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("expected directory %s to exist: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", dir)
		}
	}
}

func TestResolve_EnvLayout(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("RTVORTEX_HOME", tmpDir)

	env, err := rtenv.Resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env.ConfigDir != tmpDir+"/config" {
		t.Errorf("expected config=%s/config, got %s", tmpDir, env.ConfigDir)
	}
	if env.DataDir != tmpDir+"/data" {
		t.Errorf("expected data=%s/data, got %s", tmpDir, env.DataDir)
	}
	if env.TempDir != tmpDir+"/temp" {
		t.Errorf("expected temp=%s/temp, got %s", tmpDir, env.TempDir)
	}
	if env.ModelsDir != tmpDir+"/models" {
		t.Errorf("expected models=%s/models, got %s", tmpDir, env.ModelsDir)
	}
}

func TestResolve_Hostname(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("RTVORTEX_HOME", tmpDir)

	env, err := rtenv.Resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Hostname == "" {
		t.Error("expected non-empty hostname")
	}
}

func TestBinaryName(t *testing.T) {
	name := rtenv.BinaryName()
	if runtime.GOOS == "windows" {
		if name != "RTVortexGo.exe" {
			t.Errorf("expected RTVortexGo.exe, got %s", name)
		}
	} else {
		if name != "RTVortexGo" {
			t.Errorf("expected RTVortexGo, got %s", name)
		}
	}
}

func TestBinarySuffix(t *testing.T) {
	suffix := rtenv.BinarySuffix()
	if suffix == "" {
		t.Error("expected non-empty suffix")
	}
	expected := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		expected += ".exe"
	}
	if suffix != expected {
		t.Errorf("expected %s, got %s", expected, suffix)
	}
}
