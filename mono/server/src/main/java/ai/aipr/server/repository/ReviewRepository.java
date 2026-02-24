package ai.aipr.server.repository;

import ai.aipr.server.dto.ReviewComment;
import ai.aipr.server.dto.ReviewMetrics;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.model.PageRequest;
import ai.aipr.server.model.PagedResult;
import ai.aipr.server.model.ReviewFilter;
import ai.aipr.server.persistence.Persister;
import org.jetbrains.annotations.NotNull;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;

import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

/**
 * Repository for review data backed by PostgreSQL via {@link Persister}.
 */
@Repository
public class ReviewRepository {

    private final Persister db;

    public ReviewRepository(Persister db) {
        this.db = db;
    }

    // =====================================================================
    // Save
    // =====================================================================

    public void save(@NotNull ReviewResponse review) {
        String id = review.reviewId() != null ? review.reviewId() : UUID.randomUUID().toString();

        db.update("""
            MERGE INTO reviews (id, repository_id, pr_number, status, summary, created_at)
            KEY (id)
            VALUES (CAST(? AS UUID), CAST(? AS UUID), ?, ?, ?, NOW())
            """,
            id, review.repoId(), review.prNumber(), review.status(), review.summary()
        );

        if (review.comments() != null) {
            for (ReviewComment c : review.comments()) {
                db.update("""
                    MERGE INTO review_comments
                      (id, review_id, file_path, line_number, end_line_number, severity, category, source, message, suggestion, confidence)
                    KEY (id)
                    VALUES (CAST(? AS UUID), CAST(? AS UUID), ?, ?, ?, ?, ?, ?, ?, ?, ?)
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

        if (review.metrics() != null) {
            ReviewMetrics m = review.metrics();
            db.update("""
                MERGE INTO review_metrics
                  (id, review_id, total_files, lines_added, lines_deleted,
                   tokens_prompt, tokens_completion, llm_latency_ms, total_latency_ms,
                   heuristic_findings, llm_findings)
                KEY (review_id)
                VALUES (CAST(? AS UUID), CAST(? AS UUID), ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                UUID.randomUUID().toString(), id,
                m.filesAnalyzed(), m.linesAdded(), m.linesRemoved(),
                m.promptTokens() != null ? m.promptTokens() : m.tokensUsed(),
                m.completionTokens() != null ? m.completionTokens() : 0,
                m.llmLatencyMs() != null ? m.llmLatencyMs() : m.latencyMs(),
                m.latencyMs(),
                m.totalFindings(), m.totalFindings()
            );
        }
    }

    // =====================================================================
    // Queries
    // =====================================================================

    public Optional<ReviewResponse> findById(String reviewId) {
        return db.queryForOptional("SELECT * FROM reviews WHERE id = CAST(? AS UUID)", REVIEW_ROW_MAPPER, reviewId)
                .map(this::hydrate);
    }

    public List<ReviewResponse> findByRepoId(String repoId) {
        return db.query(
            "SELECT * FROM reviews WHERE repository_id = CAST(? AS UUID) ORDER BY created_at DESC",
            REVIEW_ROW_MAPPER, repoId).stream().map(this::hydrate).toList();
    }

    public List<ReviewResponse> findByRepoId(String repoId, int page, int size) {
        return db.query(
            "SELECT * FROM reviews WHERE repository_id = CAST(? AS UUID) ORDER BY created_at DESC LIMIT ? OFFSET ?",
            REVIEW_ROW_MAPPER, repoId, size, (long) page * size).stream().map(this::hydrate).toList();
    }

    public List<ReviewResponse> findByRepoIdAndPrNumber(String repoId, int prNumber) {
        return db.query(
            "SELECT * FROM reviews WHERE repository_id = CAST(? AS UUID) AND pr_number = ? ORDER BY created_at DESC",
            REVIEW_ROW_MAPPER, repoId, prNumber).stream().map(this::hydrate).toList();
    }

    public PagedResult<ReviewResponse> findByFilter(@NotNull ReviewFilter filter, PageRequest pageRequest) {
        StringBuilder sql = new StringBuilder("SELECT * FROM reviews WHERE 1=1");
        StringBuilder countSql = new StringBuilder("SELECT COUNT(*) FROM reviews WHERE 1=1");
        List<Object> params = new ArrayList<>();

        if (filter.repoId() != null) {
            sql.append(" AND repository_id = CAST(? AS UUID)");
            countSql.append(" AND repository_id = CAST(? AS UUID)");
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

        int total = db.queryScalar(countSql.toString(), Integer.class, 0, params.toArray());

        sql.append(" ORDER BY created_at DESC LIMIT ? OFFSET ?");
        params.add(pageRequest.size());
        params.add((long) pageRequest.page() * pageRequest.size());

        List<ReviewResponse> page = db.jdbc().query(sql.toString(), REVIEW_ROW_MAPPER, params.toArray())
                .stream().map(this::hydrate).toList();

        return new PagedResult<>(page, total, pageRequest.page(), pageRequest.size());
    }

    // =====================================================================
    // Delete
    // =====================================================================

    public void deleteById(String reviewId) {
        db.update("DELETE FROM reviews WHERE id = CAST(? AS UUID)", reviewId);
    }

    public int deleteByRepoId(String repoId) {
        return db.update("DELETE FROM reviews WHERE repository_id = CAST(? AS UUID)", repoId);
    }

    // =====================================================================
    // Count
    // =====================================================================

    public long countByRepoId(String repoId) {
        return db.queryScalar("SELECT COUNT(*) FROM reviews WHERE repository_id = CAST(? AS UUID)",
                Long.class, 0L, repoId);
    }

    // =====================================================================
    // Row Mappers & Helpers
    // =====================================================================

    private ReviewResponse hydrate(@NotNull ReviewResponse base) {
        List<ReviewComment> comments = db.query(
            "SELECT * FROM review_comments WHERE review_id = CAST(? AS UUID) ORDER BY file_path, line_number",
            COMMENT_ROW_MAPPER, base.reviewId());

        ReviewMetrics metrics = db.queryForOptional(
            "SELECT * FROM review_metrics WHERE review_id = CAST(? AS UUID)",
            METRICS_ROW_MAPPER, base.reviewId()).orElse(null);

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
            .promptTokens(rs.getInt("tokens_prompt"))
            .completionTokens(rs.getInt("tokens_completion"))
            .latencyMs(rs.getInt("total_latency_ms"))
            .llmLatencyMs(rs.getInt("llm_latency_ms"))
            .totalFindings(rs.getInt("heuristic_findings") + rs.getInt("llm_findings"))
            .build();
}
