package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── AssetRepository ─────────────────────────────────────────────────────────

// AssetType enumerates supported asset types.
type AssetType string

const (
	AssetTypePDF      AssetType = "pdf"
	AssetTypeImage    AssetType = "image"
	AssetTypeAudio    AssetType = "audio"
	AssetTypeVideo    AssetType = "video"
	AssetTypeWebpage  AssetType = "webpage"
	AssetTypeDocument AssetType = "document"
)

// Asset represents an uploaded file or URL that has been indexed.
type Asset struct {
	ID           uuid.UUID `json:"id"`
	RepoID       uuid.UUID `json:"repo_id"`
	AssetType    string    `json:"asset_type"`
	SourceURL    string    `json:"source_url,omitempty"`
	FileName     string    `json:"file_name,omitempty"`
	MIMEType     string    `json:"mime_type,omitempty"`
	SizeBytes    int64     `json:"size_bytes"`
	ChunksCount  int       `json:"chunks_count"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	Metadata     string    `json:"metadata,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AssetRepository handles persistence for repo_assets.
type AssetRepository struct {
	pool *pgxpool.Pool
}

// NewAssetRepository creates an asset repository.
func NewAssetRepository(pool *pgxpool.Pool) *AssetRepository {
	return &AssetRepository{pool: pool}
}

// Create inserts a new asset record in "processing" status.
func (r *AssetRepository) Create(ctx context.Context, asset *Asset) error {
	if asset.ID == uuid.Nil {
		asset.ID = uuid.New()
	}
	now := time.Now().UTC()
	asset.CreatedAt = now
	asset.UpdatedAt = now
	asset.Status = "processing"

	_, err := r.pool.Exec(ctx,
		`INSERT INTO repo_assets (id, repo_id, asset_type, source_url, file_name, mime_type, size_bytes, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		asset.ID, asset.RepoID, asset.AssetType, asset.SourceURL, asset.FileName,
		asset.MIMEType, asset.SizeBytes, asset.Status, asset.CreatedAt, asset.UpdatedAt,
	)
	return err
}

// UpdateStatus updates the processing status of an asset.
func (r *AssetRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status, errorMsg string, chunks int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE repo_assets SET status = $1, error_message = $2, chunks_count = $3, updated_at = NOW()
		 WHERE id = $4`,
		status, errorMsg, chunks, id,
	)
	return err
}

// UpdateMeta updates the file metadata after a URL fetch (file_name, mime_type,
// size_bytes, asset_type) so the UI shows real values instead of 0 B / blank.
func (r *AssetRepository) UpdateMeta(ctx context.Context, id uuid.UUID, fileName, mimeType, assetType string, sizeBytes int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE repo_assets SET file_name = $1, mime_type = $2, asset_type = $3, size_bytes = $4, updated_at = NOW()
		 WHERE id = $5`,
		fileName, mimeType, assetType, sizeBytes, id,
	)
	return err
}

// UpdateMetadata stores arbitrary JSON metadata (e.g. image thumbnails) on an asset.
func (r *AssetRepository) UpdateMetadata(ctx context.Context, id uuid.UUID, metadata string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE repo_assets SET metadata = $1, updated_at = NOW() WHERE id = $2`,
		metadata, id,
	)
	return err
}

// ListByRepo returns all assets for a given repository.
func (r *AssetRepository) ListByRepo(ctx context.Context, repoID uuid.UUID, limit int) ([]Asset, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, repo_id, asset_type, COALESCE(source_url,''), COALESCE(file_name,''),
		        COALESCE(mime_type,''), size_bytes, chunks_count, status,
		        COALESCE(error_message,''), COALESCE(metadata::text,''), created_at
		 FROM repo_assets WHERE repo_id = $1 ORDER BY created_at DESC LIMIT $2`,
		repoID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assets := make([]Asset, 0)
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.RepoID, &a.AssetType, &a.SourceURL,
			&a.FileName, &a.MIMEType, &a.SizeBytes, &a.ChunksCount,
			&a.Status, &a.ErrorMessage, &a.Metadata, &a.CreatedAt); err != nil {
			continue
		}
		assets = append(assets, a)
	}
	return assets, nil
}

// GetByID retrieves a single asset by its ID.
func (r *AssetRepository) GetByID(ctx context.Context, id uuid.UUID) (*Asset, error) {
	var a Asset
	err := r.pool.QueryRow(ctx,
		`SELECT id, repo_id, asset_type, COALESCE(source_url,''), COALESCE(file_name,''),
		        COALESCE(mime_type,''), size_bytes, chunks_count, status,
		        COALESCE(error_message,''), COALESCE(metadata::text,''), created_at
		 FROM repo_assets WHERE id = $1`,
		id,
	).Scan(&a.ID, &a.RepoID, &a.AssetType, &a.SourceURL,
		&a.FileName, &a.MIMEType, &a.SizeBytes, &a.ChunksCount,
		&a.Status, &a.ErrorMessage, &a.Metadata, &a.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Delete removes an asset record.
func (r *AssetRepository) Delete(ctx context.Context, id, repoID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM repo_assets WHERE id = $1 AND repo_id = $2`,
		id, repoID,
	)
	return err
}

// CountByRepo returns the number of assets for a given repo.
func (r *AssetRepository) CountByRepo(ctx context.Context, repoID uuid.UUID) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM repo_assets WHERE repo_id = $1`,
		repoID,
	).Scan(&count)
	return count, err
}
