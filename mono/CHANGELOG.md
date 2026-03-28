# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

#### C++ Engine — TMS Ingestion Pipeline
- **Batched streaming ingestion** (`82a162e`): Rewrote `ingestRepository()` to process
  files in batches of 5,000, preventing OOM on large repositories. A 130 GB repo with
  208K files / 9.5M chunks now indexes at ~5 GB RSS instead of 52 GB (OOM kill).
- **Parallel ONNX embedding workers** (`1feda76`): Multiple ONNX sessions run
  concurrently across CPU cores. Auto-configures to `cores/4` workers (e.g., 6 workers
  on 24-core machine). ~5x embedding throughput improvement.
- **`listFiles()` API** on `RepoParser`: Lightweight file discovery that collects
  indexable paths without loading content, enabling batched ingestion.
- **Configurable ONNX threading**: `EmbeddingConfig::onnx_intra_op_threads` and
  `num_parallel_workers` fields for tuning inference parallelism.

#### C++ Engine — Hierarchical Chunking (`6a806f1`)
- **HierarchyBuilder**: Generates file-summary chunks with structural context
  (module path, imports, exports) appended to each batch.
- **ChunkPrefixer**: Applies repository-aware structural prefixes to chunk content
  before embedding, improving retrieval relevance.
- **RepoManifest**: Lightweight dependency graph metadata built once per ingestion.

#### C++ Engine — Knowledge Graph & Memory Accounts
- **KnowledgeGraph** (SQLite-backed): Builds IMPORTS / CALLS / CONTAINS edges from
  parsed chunks, enabling graph-augmented retrieval.
- **MemoryAccountClassifier**: Routes chunks into dev / ops / security / history
  accounts based on file path and content heuristics. Query-time account filtering
  narrows search to relevant code regions.
- **Confidence Gate**: Zero-LLM fast path — when retrieval similarity exceeds a
  configurable threshold, skips the LLM round-trip entirely.

#### C++ Engine — Metrics (`9f9bb8f`)
- 20+ Prometheus-compatible metrics: embedding latency, FAISS load status, MiniLM
  readiness, confidence gate scores, LLM avoidance rate, CMA scores.
- `metrics::Registry` singleton with counters, gauges, and histograms.
- Real-time metrics streaming to Go server via gRPC `StreamMetrics` RPC.

#### Go Server — Repository Management UI (`0ef527b`)
- **Web UI** for repository management: index / reindex / reclone buttons with
  confirmation dialogs and branch selector dropdown.
- **Branch listing endpoint**: `GET /api/v1/repos/{id}/branches` fetches remote
  branches via `git ls-remote`.
- **Index action routing**: `index_action` and `target_branch` proto fields (18, 19)
  control whether the engine clones fresh, reindexes existing, or reclones.
- Fixed root cause where every reindex re-cloned: engine now detects existing local
  clones and skips cloning for `reindex` action.

#### Go Server — Metrics Dashboard
- Real-time engine metrics dashboard in Web UI via Server-Sent Events.
- `StreamMetrics` gRPC subscription with 1-second polling interval.
- Displays FAISS status, MiniLM readiness, embedding backend, confidence gate
  scores, and LLM avoidance rate.

### Fixed
- **OOM kill on large repos**: 130 GB repo with 9.5M chunks no longer exhausts
  62 GB of RAM. Batched pipeline caps peak RSS at ~5-6 GB.
- **Reindex always re-cloned**: `handlers.go` always passed `repo.CloneURL` to the
  engine, causing unnecessary re-clones. Now passes local path for reindex.

### Changed
- `ingestRepository()` signature unchanged but internals completely rewritten
  for batched streaming (backward compatible).
- ONNX session initialization now accepts configurable thread count instead of
  hardcoded `SetIntraOpNumThreads(4)`.

## [0.1.0] - TBD

### Added
- Foundation release
- Basic indexing pipeline
- PR review workflow
- GitHub integration

---

## Release Notes Format

For each release, document:

1. **Breaking Changes**: Any changes that require user action
2. **New Features**: New capabilities added
3. **Improvements**: Enhancements to existing features
4. **Bug Fixes**: Issues resolved
5. **Security**: Security-related changes
6. **Dependencies**: Notable dependency updates
