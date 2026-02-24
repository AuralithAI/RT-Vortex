package ai.aipr.server.repository;

import ai.aipr.server.dto.IndexInfo;
import ai.aipr.server.dto.IndexState;
import ai.aipr.server.dto.IndexStatus;
import ai.aipr.server.persistence.Persister;
import org.jetbrains.annotations.NotNull;
import org.springframework.dao.DataAccessException;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;

import java.sql.Timestamp;
import java.time.Instant;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

/**
 * Repository for indexing status and information backed by PostgreSQL via {@link Persister}.
 */
@Repository
public class IndexRepository {

    private final Persister db;

    public IndexRepository(Persister db) {
        this.db = db;
    }

    // =====================================================================
    // IndexStatus (index_jobs table)
    // =====================================================================

    public void saveStatus(@NotNull IndexStatus status) {
        String id = status.jobId() != null ? status.jobId() : UUID.randomUUID().toString();
        db.update("""
            INSERT INTO index_jobs (id, repository_id, job_type, status, progress, files_processed, error_message, started_at, completed_at, created_at)
            VALUES (?::uuid, ?::uuid, 'full', ?, ?, ?, ?, ?, ?, NOW())
            ON CONFLICT (id) DO UPDATE
              SET status          = EXCLUDED.status,
                  progress        = EXCLUDED.progress,
                  files_processed = EXCLUDED.files_processed,
                  error_message   = EXCLUDED.error_message,
                  started_at      = EXCLUDED.started_at,
                  completed_at    = EXCLUDED.completed_at
            """,
            id, status.repoId(),
            status.state() != null ? status.state().name().toLowerCase() : "pending",
            status.progress(),
            status.filesProcessed(),
            status.error(),
            status.startTime() != null ? Timestamp.from(status.startTime()) : null,
            status.endTime() != null ? Timestamp.from(status.endTime()) : null
        );
    }

    public void updateStatus(String jobId, @NotNull IndexState state, int progress, String message) {
        db.update("""
            UPDATE index_jobs SET status = ?, progress = ?, error_message = ? WHERE id = ?::uuid
            """,
            state.name().toLowerCase(), progress, message, jobId);
    }

    public Optional<IndexStatus> findStatusByJobId(String jobId) {
        try {
            return db.queryForOptional("SELECT * FROM index_jobs WHERE id = ?::uuid", STATUS_ROW_MAPPER, jobId);
        } catch (DataAccessException e) {
            return Optional.empty();
        }
    }

    public Optional<IndexStatus> findStatusById(String jobId) {
        return findStatusByJobId(jobId);
    }

    public List<IndexStatus> findActiveJobsByRepoId(String repoId) {
        return db.query(
            "SELECT * FROM index_jobs WHERE repository_id = ?::uuid AND status IN ('pending', 'running') ORDER BY created_at DESC",
            STATUS_ROW_MAPPER, repoId);
    }

    public void deleteStatus(String jobId) {
        db.update("DELETE FROM index_jobs WHERE id = ?::uuid", jobId);
    }

    // =====================================================================
    // IndexInfo (index_stats table)
    // =====================================================================

    public void saveInfo(@NotNull IndexInfo info) {
        db.update("""
            INSERT INTO index_stats (id, repository_id, index_version, total_files, indexed_files, total_chunks, total_symbols, last_commit, last_indexed_at, updated_at)
            VALUES (?::uuid, ?::uuid, ?, ?, ?, ?, ?, ?, ?, NOW())
            ON CONFLICT (repository_id) DO UPDATE
              SET index_version   = EXCLUDED.index_version,
                  total_files     = EXCLUDED.total_files,
                  indexed_files   = EXCLUDED.indexed_files,
                  total_chunks    = EXCLUDED.total_chunks,
                  total_symbols   = EXCLUDED.total_symbols,
                  last_commit     = EXCLUDED.last_commit,
                  last_indexed_at = EXCLUDED.last_indexed_at,
                  updated_at      = NOW()
            """,
            UUID.randomUUID().toString(), info.repoId(),
            info.indexVersion(),
            info.fileCount(), info.fileCount(),
            info.chunkCount(), info.symbolCount(),
            info.commitSha(),
            info.lastIndexedAt() != null ? Timestamp.from(info.lastIndexedAt()) : null
        );
    }

    public Optional<IndexInfo> findInfoByRepoId(String repoId) {
        try {
            return db.queryForOptional("SELECT * FROM index_stats WHERE repository_id = ?::uuid", INFO_ROW_MAPPER, repoId);
        } catch (DataAccessException e) {
            return Optional.empty();
        }
    }

    public boolean isIndexed(String repoId) {
        return db.queryScalar("SELECT COUNT(*) FROM index_stats WHERE repository_id = ?::uuid",
                Integer.class, 0, repoId) > 0;
    }

    public void deleteInfo(String repoId) {
        db.update("DELETE FROM index_stats WHERE repository_id = ?::uuid", repoId);
    }

    public void deleteByRepoId(String repoId) {
        try {
            db.update("DELETE FROM index_stats WHERE repository_id = ?::uuid", repoId);
        } catch (DataAccessException e) {
            // ignore if table doesn't exist or UUID cast fails
        }
        try {
            db.update("DELETE FROM index_jobs WHERE repository_id = ?::uuid", repoId);
        } catch (DataAccessException e) {
            // ignore if UUID cast fails
        }
    }

    public List<IndexInfo> listAllIndexes() {
        return db.query("SELECT * FROM index_stats ORDER BY updated_at DESC", INFO_ROW_MAPPER);
    }

    public int cleanupOldJobs(long maxAgeMs) {
        Timestamp cutoff = Timestamp.from(Instant.now().minusMillis(maxAgeMs));
        return db.update("DELETE FROM index_jobs WHERE status IN ('completed', 'failed') AND completed_at < ?", cutoff);
    }

    // =====================================================================
    // Row Mappers
    // =====================================================================

    private static final RowMapper<IndexStatus> STATUS_ROW_MAPPER = (rs, rowNum) -> {
        Timestamp started = rs.getTimestamp("started_at");
        Timestamp completed = rs.getTimestamp("completed_at");
        return IndexStatus.builder()
            .jobId(rs.getString("id"))
            .repoId(rs.getString("repository_id"))
            .state(parseState(rs.getString("status")))
            .progress((int) rs.getDouble("progress"))
            .filesProcessed(rs.getInt("files_processed"))
            .error(rs.getString("error_message"))
            .startTime(started != null ? started.toInstant() : null)
            .endTime(completed != null ? completed.toInstant() : null)
            .build();
    };

    private static final RowMapper<IndexInfo> INFO_ROW_MAPPER = (rs, rowNum) -> {
        Timestamp lastIndexed = rs.getTimestamp("last_indexed_at");
        Timestamp created = rs.getTimestamp("created_at");
        Timestamp updated = rs.getTimestamp("updated_at");
        return IndexInfo.builder()
            .repoId(rs.getString("repository_id"))
            .indexVersion(rs.getString("index_version"))
            .commitSha(rs.getString("last_commit"))
            .fileCount(rs.getInt("total_files"))
            .chunkCount(rs.getInt("total_chunks"))
            .symbolCount(rs.getInt("total_symbols"))
            .lastIndexedAt(lastIndexed != null ? lastIndexed.toInstant() : null)
            .createdAt(created != null ? created.toInstant() : null)
            .updatedAt(updated != null ? updated.toInstant() : null)
            .state(IndexState.COMPLETED)
            .build();
    };

    private static IndexState parseState(String value) {
        if (value == null) return IndexState.PENDING;
        try {
            return IndexState.valueOf(value.toUpperCase());
        } catch (IllegalArgumentException e) {
            return IndexState.PENDING;
        }
    }
}
