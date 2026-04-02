package keychain

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationConfig controls the FileVault → Keychain migration.
type MigrationConfig struct {
	// DryRun logs what would happen without making changes.
	DryRun bool

	// SkipCategories lists secret categories to skip during migration.
	SkipCategories []string
}

// MigrateResult summarizes the outcome of a vault migration.
type MigrateResult struct {
	UserID         uuid.UUID
	Migrated       int
	Skipped        int
	Errors         int
	ErrorDetails   []string
	AlreadyExists  int
}

// FileVaultReader abstracts reading from the old FileVault for migration.
type FileVaultReader interface {
	Get(key string) (string, error)
	List() ([]string, error)
	GetAll() (map[string]string, error)
}

// MigrateUserSecrets migrates all secrets for a single user from the old
// FileVault (via the UserScopedVault prefix convention) to the new keychain.
//
// This reads all secrets from the FileVault with the user's vault token
// prefix, strips the prefix, and writes them to the keychain. Existing
// secrets in the keychain are not overwritten (CRDT version check).
func MigrateUserSecrets(
	ctx context.Context,
	svc *Service,
	oldVault FileVaultReader,
	userID uuid.UUID,
	cfg MigrationConfig,
) (*MigrateResult, error) {
	result := &MigrateResult{UserID: userID}

	// Build skip set.
	skipSet := make(map[string]bool, len(cfg.SkipCategories))
	for _, c := range cfg.SkipCategories {
		skipSet[c] = true
	}

	// Ensure the user has a keychain.
	_, err := svc.store.GetKeychain(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("keychain migration: user %s has no keychain — init first", userID)
	}

	// Read all secrets from the old vault.
	allSecrets, err := oldVault.GetAll()
	if err != nil {
		return nil, fmt.Errorf("keychain migration: read old vault: %w", err)
	}

	for key, value := range allSecrets {
		if value == "" {
			result.Skipped++
			continue
		}

		category := inferCategory(key)
		if skipSet[category] {
			result.Skipped++
			continue
		}

		if cfg.DryRun {
			slog.Info("keychain migration [dry-run]: would migrate",
				"user_id", userID, "key", key, "category", category)
			result.Migrated++
			continue
		}

		err := svc.PutSecret(ctx, userID, key, category, []byte(value), "{}")
		if err != nil {
			result.Errors++
			result.ErrorDetails = append(result.ErrorDetails, fmt.Sprintf("%s: %v", key, err))
			slog.Warn("keychain migration: failed to write secret",
				"user_id", userID, "key", key, "error", err)
			continue
		}
		result.Migrated++
	}

	slog.Info("keychain migration complete",
		"user_id", userID,
		"migrated", result.Migrated,
		"skipped", result.Skipped,
		"errors", result.Errors,
		"dry_run", cfg.DryRun,
	)
	return result, nil
}

// BulkMigrate migrates all users from the old FileVault to keychain.
// It expects a map of userID → vault-scoped reader (one per user).
func BulkMigrate(
	ctx context.Context,
	pool *pgxpool.Pool,
	svc *Service,
	vaultReaders map[uuid.UUID]FileVaultReader,
	cfg MigrationConfig,
) ([]MigrateResult, error) {
	var results []MigrateResult

	for userID, reader := range vaultReaders {
		result, err := MigrateUserSecrets(ctx, svc, reader, userID, cfg)
		if err != nil {
			slog.Warn("keychain bulk migration: user failed",
				"user_id", userID, "error", err)
			results = append(results, MigrateResult{
				UserID: userID, Errors: 1,
				ErrorDetails: []string{err.Error()},
			})
			continue
		}
		results = append(results, *result)
	}
	return results, nil
}

func inferCategory(key string) string {
	if strings.HasPrefix(key, "vcs.") {
		return "vcs"
	}
	if strings.HasPrefix(key, "llm.") || strings.HasPrefix(key, "llm_") {
		return "llm"
	}
	if strings.HasPrefix(key, "mcp:") || strings.HasPrefix(key, "mcp.") {
		return "mcp"
	}
	if strings.HasPrefix(key, "embed_key.") {
		return "embedding"
	}
	return "custom"
}
