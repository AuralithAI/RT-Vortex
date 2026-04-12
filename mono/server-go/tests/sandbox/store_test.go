package sandbox_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/sandbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool returns a pgxpool connected to the test database.
// Skips the test when SANDBOX_TEST_DB is not set.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SANDBOX_TEST_DB")
	if dsn == "" {
		t.Skip("SANDBOX_TEST_DB not set — skipping store integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedUserAndTask creates a temporary user + swarm_task so FK constraints pass.
// Returns (userID, taskID) and registers cleanup.
func seedUserAndTask(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New()
	taskID := uuid.New()
	repoID := uuid.New().String()

	_, err := pool.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, $3)`,
		userID, fmt.Sprintf("test-%s@sandbox.test", userID), "sandbox-test-user")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO swarm_tasks (id, repo_id, description) VALUES ($1, $2, $3)`,
		taskID, repoID, "sandbox store test task")
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM swarm_builds WHERE task_id = $1", taskID)
		_, _ = pool.Exec(ctx, "DELETE FROM swarm_tasks WHERE id = $1", taskID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID)
	})
	return userID, taskID
}

func TestBuildStore_InsertAndGet(t *testing.T) {
	pool := testPool(t)
	store := sandbox.NewBuildStore(pool)
	ctx := context.Background()
	userID, taskID := seedUserAndTask(t, pool)

	rec := &sandbox.BuildRecord{
		ID:          uuid.New(),
		TaskID:      taskID,
		RepoID:      uuid.New().String(),
		UserID:      &userID,
		BuildSystem: "go",
		Command:     "go test ./...",
		BaseImage:   "golang:1.22",
		Status:      "running",
		SecretNames: []string{"TOKEN_A"},
		SandboxMode: true,
	}

	if err := store.InsertBuild(ctx, rec); err != nil {
		t.Fatalf("InsertBuild: %v", err)
	}

	got, err := store.GetBuild(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetBuild: %v", err)
	}
	if got.Status != "running" {
		t.Errorf("Status = %q, want running", got.Status)
	}
	if got.BuildSystem != "go" {
		t.Errorf("BuildSystem = %q, want go", got.BuildSystem)
	}

	// Complete the build.
	if err := store.CompleteBuild(ctx, rec.ID, "success", 0, "ALL PASS", 1234); err != nil {
		t.Fatalf("CompleteBuild: %v", err)
	}

	got, err = store.GetBuild(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetBuild after complete: %v", err)
	}
	if got.Status != "success" {
		t.Errorf("Status = %q, want success", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", got.ExitCode)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt should not be nil after completion")
	}
}

func TestBuildStore_GetBuildByTask(t *testing.T) {
	pool := testPool(t)
	store := sandbox.NewBuildStore(pool)
	ctx := context.Background()
	userID, taskID := seedUserAndTask(t, pool)

	rec := &sandbox.BuildRecord{
		ID:          uuid.New(),
		TaskID:      taskID,
		RepoID:      uuid.New().String(),
		UserID:      &userID,
		BuildSystem: "node",
		Command:     "npm test",
		BaseImage:   "node:20",
		Status:      "running",
		SecretNames: []string{},
		SandboxMode: true,
	}

	if err := store.InsertBuild(ctx, rec); err != nil {
		t.Fatalf("InsertBuild: %v", err)
	}

	got, err := store.GetBuildByTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetBuildByTask: %v", err)
	}
	if got.ID != rec.ID {
		t.Errorf("ID = %s, want %s", got.ID, rec.ID)
	}
}

func TestBuildStore_ListBuildsByRepo(t *testing.T) {
	pool := testPool(t)
	store := sandbox.NewBuildStore(pool)
	ctx := context.Background()

	repoID := uuid.New().String()
	ids := make([]uuid.UUID, 3)
	for i := range ids {
		userID, taskID := seedUserAndTask(t, pool)
		ids[i] = uuid.New()
		rec := &sandbox.BuildRecord{
			ID:          ids[i],
			TaskID:      taskID,
			RepoID:      repoID,
			UserID:      &userID,
			BuildSystem: "go",
			Command:     "go build",
			BaseImage:   "golang:1.22",
			Status:      "success",
			SecretNames: []string{},
			SandboxMode: true,
		}
		if err := store.InsertBuild(ctx, rec); err != nil {
			t.Fatalf("InsertBuild[%d]: %v", i, err)
		}
	}

	builds, err := store.ListBuildsByRepo(ctx, repoID, 10)
	if err != nil {
		t.Fatalf("ListBuildsByRepo: %v", err)
	}
	if len(builds) < 3 {
		t.Errorf("got %d builds, want >= 3", len(builds))
	}
}

func TestBuildStore_IncrementRetry(t *testing.T) {
	pool := testPool(t)
	store := sandbox.NewBuildStore(pool)
	ctx := context.Background()
	userID, taskID := seedUserAndTask(t, pool)

	rec := &sandbox.BuildRecord{
		ID:          uuid.New(),
		TaskID:      taskID,
		RepoID:      uuid.New().String(),
		UserID:      &userID,
		BuildSystem: "go",
		Command:     "go build",
		BaseImage:   "golang:1.22",
		Status:      "running",
		SecretNames: []string{},
		SandboxMode: true,
	}
	if err := store.InsertBuild(ctx, rec); err != nil {
		t.Fatalf("InsertBuild: %v", err)
	}

	if err := store.IncrementRetry(ctx, rec.ID); err != nil {
		t.Fatalf("IncrementRetry: %v", err)
	}

	got, err := store.GetBuild(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetBuild: %v", err)
	}
	if got.RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", got.RetryCount)
	}
}

func TestBuildStore_GetBuild_NotFound(t *testing.T) {
	pool := testPool(t)
	store := sandbox.NewBuildStore(pool)

	_, err := store.GetBuild(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error for non-existent build, got nil")
	}
}
