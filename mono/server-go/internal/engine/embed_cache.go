package engine

import (
	"context"
	"encoding/binary"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

const embedCacheTTL = 24 * time.Hour

// EmbedCacheService provides a Redis-backed L2 embedding cache.
// The C++ engine calls Get before hitting the external embed provider
// and calls Put after a successful embed to warm the cache.
type EmbedCacheService struct {
	rdb *redis.Client
}

// NewEmbedCacheService creates a new embedding cache backed by Redis.
func NewEmbedCacheService(rdb *redis.Client) *EmbedCacheService {
	return &EmbedCacheService{rdb: rdb}
}

func embedCacheKey(repoID, chunkHash string) string {
	return "embed:" + repoID + ":" + chunkHash
}

// Get looks up a cached embedding vector. Returns nil if not found.
func (s *EmbedCacheService) Get(ctx context.Context, repoID, chunkHash string) ([]float32, error) {
	data, err := s.rdb.Get(ctx, embedCacheKey(repoID, chunkHash)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return deserializeFloat32(data), nil
}

// Put stores an embedding vector in Redis with a 24h TTL.
func (s *EmbedCacheService) Put(ctx context.Context, repoID, chunkHash string, vec []float32) error {
	return s.rdb.Set(ctx, embedCacheKey(repoID, chunkHash), serializeFloat32(vec), embedCacheTTL).Err()
}

func serializeFloat32(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func deserializeFloat32(data []byte) []float32 {
	n := len(data) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return out
}
