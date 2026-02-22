package ai.aipr.server.repository;

import ai.aipr.server.dto.IndexInfo;
import ai.aipr.server.dto.IndexState;
import ai.aipr.server.dto.IndexStatus;
import org.springframework.dao.EmptyResultDataAccessException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;

import java.sql.Timestamp;
import java.time.Instant;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

/**
 * Repository for indexing status and information backed by PostgreSQL via {@link JdbcTemplate}.
 * Maps to the {@code index_jobs} and {@code index_stats} tables created by
 * {@code V1__initial_schema.sql}.
 */
@Repository
public class IndexRepository {

    private final JdbcTemplate jdbc;

    public IndexRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    // =====================================================================
    // IndexStatus (index_jobs table)
    // =====================================================================

    public void saveStatus(IndexStatus status) {
        String id = status.jobId() != null ? status.jobId() : UUID.randomUUID().toString();
        jdbc.update("""
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

    public void updateStatus(String jobId, IndexState state, int progress, String message) {
        jdbc.update("""
            UPDATE index_jobs
               SET status = ?, progress = ?, error_message = ?
             WHERE id = ?::uuid
            """,
            state.name().toLowerCase(), progress, message, jobId
        );
    }

    public Optional<IndexStatus> findStatusByJobId(String jobId) {
        try {
            return Optional.ofNullable(
                jdbc.queryForObject("SELECT * FROM index_jobs WHERE id = ?::uuid", STATUS_ROW_MAPPER, jobId));
        } catch (EmptyResultDataAccessException e) {
            return Optional.empty();
        }
    }

    public Optional<IndexStatus> findStatusById(String jobId) {
        return findStatusByJobId(jobId);
    }

    public List<IndexStatus> findActiveJobsByRepoId(String repoId) {
        return jdbc.query(
            "SELECT * FROM index_jobs WHERE repository_id = ?::uuid AND status IN ('pending', 'running') ORDER BY created_at DESC",
            STATUS_ROW_MAPPER, repoId);
    }

    public void deleteStatus(String jobId) {
        jdbc.update("DELETE FROM index_jobs WHERE id = ?::uuid", jobId);
    }

    // =====================================================================
    // IndexInfo (index_stats table)
    // =====================================================================

    public void saveInfo(IndexInfo info) {
        jdbc.update("""
            INSERT INTO index_stats (id, repository_id, index_version, total_files, indexed_files, total_chunks, total_symbols, last_commit, last_indexed_at, updated_at)
            VALUES (?::uuid, ?::uuid, ?, ?, ?, ?, ?, ?, ?, NOW())
            ON CONFLICT (repository_id) DO UPDATE
              SET index_version  = EXCLUDED.index_version,
                  total_files    = EXCLUDED.total_files,
                  indexed_files  = EXCLUDED.indexed_files,
                  total_chunks   = EXCLUDED.total_chunks,
                  total_symbols  = EXCLUDED.total_symbols,
                  last_commit    = EXCLUDED.last_commit,
                  last_indexed_at = EXCLUDED.last_indexed_at,
                  updated_at     = NOW()
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
            return Optional.ofNullable(
                jdbc.queryForObject("SELECT * FROM index_stats WHERE repository_id = ?::uuid", INFO_ROW_MAPPER, repoId));
        } catch (EmptyResultDataAccessException e) {
            return Optional.empty();
        }
    }

    public boolean isIndexed(String repoId) {
        Integer count = jdbc.queryForObject(
            "SELECT COUNT(*) FROM index_stats WHERE repository_id = ?::uuid", Integer.class, repoId);
        return count != null && count > 0;
    }

    public void deleteInfo(String repoId) {
        jdbc.update("DELETE FROM index_stats WHERE repository_id = ?::uuid", repoId);
    }

    public void deleteByRepoId(String repoId) {
        jdbc.update("DELETE FROM index_stats WHERE repository_id = ?::uuid", repoId);
        jdbc.update("DELETE FROM index_jobs WHERE repository_id = ?::uuid", repoId);
    }

    public List<IndexInfo> listAllIndexes() {
        return jdbc.query("SELECT * FROM index_stats ORDER BY updated_at DESC", INFO_ROW_MAPPER);
    }

    public int cleanupOldJobs(long maxAgeMs) {
        Timestamp cutoff = Timestamp.from(Instant.now().minusMillis(maxAgeMs));
        return jdbc.update(
            "DELETE FROM index_jobs WHERE status IN ('completed', 'failed') AND completed_at < ?",
            cutoff
        );
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
