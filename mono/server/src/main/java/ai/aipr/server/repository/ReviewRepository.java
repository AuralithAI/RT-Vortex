package ai.aipr.server.repository;

import ai.aipr.server.dto.ReviewComment;
import ai.aipr.server.dto.ReviewMetrics;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.model.PageRequest;
import ai.aipr.server.model.PagedResult;
import ai.aipr.server.model.ReviewFilter;
import org.jetbrains.annotations.NotNull;
import org.springframework.dao.EmptyResultDataAccessException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;

import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

/**
 * Repository for review data backed by PostgreSQL via {@link JdbcTemplate}.
 *
 * <p>Maps to the {@code reviews}, {@code review_comments}, and
 * {@code review_metrics} tables created by {@code V1__initial_schema.sql}.</p>
 */
@Repository
public class ReviewRepository {

    private final JdbcTemplate jdbc;

    public ReviewRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    // =====================================================================
    // Save
    // =====================================================================

    public void save(@NotNull ReviewResponse review) {
        String id = review.reviewId() != null ? review.reviewId() : UUID.randomUUID().toString();

        jdbc.update("""
            INSERT INTO reviews (id, repository_id, pr_number, status, summary, created_at)
            VALUES (?::uuid, ?::uuid, ?, ?, ?, NOW())
            ON CONFLICT (id) DO UPDATE
              SET status  = EXCLUDED.status,
                  summary = EXCLUDED.summary
            """,
            id, review.repoId(), review.prNumber(), review.status(), review.summary()
        );

        // Comments
        if (review.comments() != null) {
            for (ReviewComment c : review.comments()) {
                jdbc.update("""
                    INSERT INTO review_comments
                      (id, review_id, file_path, line_number, end_line_number, severity, category, source, message, suggestion, confidence)
                    VALUES (?::uuid, ?::uuid, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    UUID.randomUUID().toString(), id,
                    c.filePath(), c.line(), c.endLine(),
                    c.severity() != null ? c.severity() : "INFO",
                    c.category() != null ? c.category() : "general",
                    c.source() != null ? c.source() : "llm",
                    c.message(), c.suggestion(), c.confidence()
                );
            }
        }

        // Metrics
        if (review.metrics() != null) {
            ReviewMetrics m = review.metrics();
            jdbc.update("""
                INSERT INTO review_metrics
                  (id, review_id, total_files, lines_added, lines_deleted,
                   tokens_prompt, tokens_completion, llm_latency_ms, total_latency_ms,
                   heuristic_findings, llm_findings)
                VALUES (?::uuid, ?::uuid, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT (review_id) DO UPDATE
                  SET total_files       = EXCLUDED.total_files,
                      tokens_prompt     = EXCLUDED.tokens_prompt,
                      tokens_completion = EXCLUDED.tokens_completion,
                      llm_latency_ms    = EXCLUDED.llm_latency_ms,
                      total_latency_ms  = EXCLUDED.total_latency_ms
                """,
                UUID.randomUUID().toString(), id,
                m.filesAnalyzed(), m.linesAdded(), m.linesRemoved(),
                m.tokensUsed(), m.tokensUsed(),   // prompt ≈ completion when not split
                m.latencyMs(), m.latencyMs(),     // llm ≈ total when not split
                m.totalFindings(), m.totalFindings()
            );
        }
    }

    // =====================================================================
    // Queries
    // =====================================================================

    public Optional<ReviewResponse> findById(String reviewId) {
        try {
            ReviewResponse r = jdbc.queryForObject(
                "SELECT * FROM reviews WHERE id = ?::uuid", REVIEW_ROW_MAPPER, reviewId);
            if (r != null) {
                return Optional.of(hydrate(r));
            }
            return Optional.empty();
        } catch (EmptyResultDataAccessException e) {
            return Optional.empty();
        }
    }

    public List<ReviewResponse> findByRepoId(String repoId) {
        List<ReviewResponse> rows = jdbc.query(
            "SELECT * FROM reviews WHERE repository_id = ?::uuid ORDER BY created_at DESC",
            REVIEW_ROW_MAPPER, repoId);
        return rows.stream().map(this::hydrate).toList();
    }

    public List<ReviewResponse> findByRepoId(String repoId, int page, int size) {
        List<ReviewResponse> rows = jdbc.query(
            "SELECT * FROM reviews WHERE repository_id = ?::uuid ORDER BY created_at DESC LIMIT ? OFFSET ?",
            REVIEW_ROW_MAPPER, repoId, size, (long) page * size);
        return rows.stream().map(this::hydrate).toList();
    }

    public List<ReviewResponse> findByRepoIdAndPrNumber(String repoId, int prNumber) {
        List<ReviewResponse> rows = jdbc.query(
            "SELECT * FROM reviews WHERE repository_id = ?::uuid AND pr_number = ? ORDER BY created_at DESC",
            REVIEW_ROW_MAPPER, repoId, prNumber);
        return rows.stream().map(this::hydrate).toList();
    }

    public PagedResult<ReviewResponse> findByFilter(@NotNull ReviewFilter filter, PageRequest pageRequest) {
        StringBuilder sql = new StringBuilder("SELECT * FROM reviews WHERE 1=1");
        StringBuilder countSql = new StringBuilder("SELECT COUNT(*) FROM reviews WHERE 1=1");
        List<Object> params = new ArrayList<>();

        if (filter.repoId() != null) {
            sql.append(" AND repository_id = ?::uuid");
            countSql.append(" AND repository_id = ?::uuid");
            params.add(filter.repoId());
        }
        if (filter.prNumber() != null) {
            sql.append(" AND pr_number = ?");
            countSql.append(" AND pr_number = ?");
            params.add(filter.prNumber());
        }
        if (filter.status() != null) {
            sql.append(" AND status = ?");
            countSql.append(" AND status = ?");
            params.add(filter.status());
        }

        int total = jdbc.queryForObject(countSql.toString(), Integer.class, params.toArray());

        sql.append(" ORDER BY created_at DESC LIMIT ? OFFSET ?");
        params.add(pageRequest.size());
        params.add((long) pageRequest.page() * pageRequest.size());

        List<ReviewResponse> rows = jdbc.query(sql.toString(), REVIEW_ROW_MAPPER, params.toArray());
        List<ReviewResponse> page = rows.stream().map(this::hydrate).toList();

        return new PagedResult<>(page, total, pageRequest.page(), pageRequest.size());
    }

    // =====================================================================
    // Delete
    // =====================================================================

    public void deleteById(String reviewId) {
        jdbc.update("DELETE FROM reviews WHERE id = ?::uuid", reviewId);
    }

    public int deleteByRepoId(String repoId) {
        return jdbc.update("DELETE FROM reviews WHERE repository_id = ?::uuid", repoId);
    }

    // =====================================================================
    // Count
    // =====================================================================

    public long countByRepoId(String repoId) {
        Long count = jdbc.queryForObject(
            "SELECT COUNT(*) FROM reviews WHERE repository_id = ?::uuid", Long.class, repoId);
        return count != null ? count : 0;
    }

    // =====================================================================
    // Row Mappers & Helpers
    // =====================================================================

    private ReviewResponse hydrate(@NotNull ReviewResponse base) {
        // Load comments
        List<ReviewComment> comments = jdbc.query(
            "SELECT * FROM review_comments WHERE review_id = ?::uuid ORDER BY file_path, line_number",
            COMMENT_ROW_MAPPER, base.reviewId());

        // Load metrics
        ReviewMetrics metrics = null;
        try {
            metrics = jdbc.queryForObject(
                "SELECT * FROM review_metrics WHERE review_id = ?::uuid",
                METRICS_ROW_MAPPER, base.reviewId());
        } catch (EmptyResultDataAccessException ignored) {}

        return ReviewResponse.builder()
                .reviewId(base.reviewId())
                .repoId(base.repoId())
                .prNumber(base.prNumber())
                .status(base.status())
                .summary(base.summary())
                .overallAssessment(base.overallAssessment())
                .comments(comments)
                .suggestions(base.suggestions())
                .metrics(metrics)
                .metadata(base.metadata())
                .build();
    }

    private static final RowMapper<ReviewResponse> REVIEW_ROW_MAPPER = (rs, rowNum) ->
        ReviewResponse.builder()
            .reviewId(rs.getString("id"))
            .repoId(rs.getString("repository_id"))
            .prNumber(rs.getInt("pr_number"))
            .status(rs.getString("status"))
            .summary(rs.getString("summary"))
            .build();

    private static final RowMapper<ReviewComment> COMMENT_ROW_MAPPER = (rs, rowNum) ->
        ReviewComment.builder()
            .filePath(rs.getString("file_path"))
            .line(rs.getInt("line_number"))
            .endLine(rs.getObject("end_line_number", Integer.class))
            .severity(rs.getString("severity"))
            .category(rs.getString("category"))
            .source(rs.getString("source"))
            .message(rs.getString("message"))
            .suggestion(rs.getString("suggestion"))
            .confidence(rs.getObject("confidence", Double.class))
            .build();

    private static final RowMapper<ReviewMetrics> METRICS_ROW_MAPPER = (rs, rowNum) ->
        ReviewMetrics.builder()
            .filesAnalyzed(rs.getInt("total_files"))
            .linesAdded(rs.getInt("lines_added"))
            .linesRemoved(rs.getInt("lines_deleted"))
            .tokensUsed(rs.getInt("tokens_prompt") + rs.getInt("tokens_completion"))
            .latencyMs(rs.getInt("total_latency_ms"))
            .totalFindings(rs.getInt("heuristic_findings") + rs.getInt("llm_findings"))
            .build();
}
