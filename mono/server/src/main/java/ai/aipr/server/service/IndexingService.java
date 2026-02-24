package ai.aipr.server.service;

import ai.aipr.server.dto.IndexInfo;
import ai.aipr.server.dto.IndexRequest;
import ai.aipr.server.dto.IndexState;
import ai.aipr.server.dto.IndexStats;
import ai.aipr.server.dto.IndexStatus;
import ai.aipr.server.engine.EngineClient;
import ai.aipr.server.repository.IndexRepository;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Async;
import org.springframework.stereotype.Service;

import java.time.Duration;
import java.time.Instant;
import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.CompletableFuture;

/**
 * Service for repository indexing operations.
 */
@Service
public class IndexingService {

    private static final Logger log = LoggerFactory.getLogger(IndexingService.class);

    private final EngineClient engineClient;
    private final IndexRepository indexRepository;

    public IndexingService(EngineClient engineClient, IndexRepository indexRepository) {
        this.engineClient = engineClient;
        this.indexRepository = indexRepository;
    }

    /**
     * Index a repository asynchronously.
     */
    @Async
    public CompletableFuture<IndexStatus> indexRepository(@NotNull IndexRequest request, boolean incremental) {
        String jobId = UUID.randomUUID().toString();
        Instant startTime = Instant.now();

        log.info("Starting {} indexing: job={}, repo={}",
                incremental ? "incremental" : "full", jobId, request.repoId());

        // Create initial status
        var status = IndexStatus.builder()
                .jobId(jobId)
                .repoId(request.repoId())
                .state(IndexState.RUNNING)
                .progress(0)
                .startTime(startTime)
                .build();

        indexRepository.saveStatus(status);

        try {
            // Clone or fetch repository
            updateStatus(jobId, IndexState.RUNNING, 5, "Fetching repository...");
            String repoPath = fetchRepository(request);

            // Scan files
            updateStatus(jobId, IndexState.RUNNING, 10, "Scanning files...");
            var scanResult = engineClient.scanRepository(repoPath, request.config());

            // Parse and chunk
            updateStatus(jobId, IndexState.RUNNING, 30, "Parsing and chunking...");
            var chunks = engineClient.chunkFiles(repoPath, scanResult.files(), incremental);

            // Generate embeddings
            updateStatus(jobId, IndexState.RUNNING, 50, "Generating embeddings...");
            var embeddings = engineClient.generateEmbeddings(chunks);

            // Build indices
            updateStatus(jobId, IndexState.RUNNING, 70, "Building indices...");
            engineClient.buildIndices(request.repoId(), chunks, embeddings);

            // Build symbol graph
            updateStatus(jobId, IndexState.RUNNING, 85, "Building symbol graph...");
            engineClient.buildSymbolGraph(request.repoId(), scanResult.symbols());

            // Finalize
            updateStatus(jobId, IndexState.RUNNING, 95, "Finalizing...");

            Instant now = Instant.now();
            long durationMs = Duration.between(startTime, now).toMillis();

            var indexStats = IndexStats.builder()
                    .totalFiles(scanResult.totalFiles())
                    .indexedFiles(scanResult.files().size())
                    .totalChunks(chunks.size())
                    .totalSymbols(scanResult.symbols().size())
                    .totalSizeBytes(scanResult.totalSizeBytes())
                    .durationMs(durationMs)
                    .build();

            var indexInfo = IndexInfo.builder()
                    .repoId(request.repoId())
                    .indexVersion("1.0")
                    .commitSha(request.commitSha())
                    .branch(request.branch())
                    .fileCount(scanResult.files().size())
                    .chunkCount(chunks.size())
                    .symbolCount(scanResult.symbols().size())
                    .lastIndexedAt(now)
                    .createdAt(now)
                    .updatedAt(now)
                    .stats(indexStats)
                    .state(IndexState.COMPLETED)
                    .build();

            indexRepository.saveInfo(indexInfo);

            // Complete
            var finalStatus = IndexStatus.builder()
                    .jobId(jobId)
                    .repoId(request.repoId())
                    .state(IndexState.COMPLETED)
                    .progress(100)
                    .startTime(startTime)
                    .endTime(now)
                    .filesProcessed(scanResult.files().size())
                    .stats(indexStats)
                    .build();

            indexRepository.saveStatus(finalStatus);

            log.info("Indexing completed: job={}, files={}, chunks={}",
                    jobId, scanResult.files().size(), chunks.size());

            return CompletableFuture.completedFuture(finalStatus);

        } catch (Exception e) {
            log.error("Indexing failed: job={}, error={}", jobId, e.getMessage(), e);

            var failedStatus = IndexStatus.builder()
                    .jobId(jobId)
                    .repoId(request.repoId())
                    .state(IndexState.FAILED)
                    .error(e.getMessage())
                    .startTime(startTime)
                    .endTime(Instant.now())
                    .build();

            indexRepository.saveStatus(failedStatus);

            return CompletableFuture.completedFuture(failedStatus);
        }
    }

    /**
     * Get indexing job status.
     */
    public Optional<IndexStatus> getStatus(String jobId) {
        return indexRepository.findStatusById(jobId);
    }

    /**
     * Get the latest indexing status for a repository.
     * Looks up by repository ID instead of job ID — returns the most recent job status.
     */
    public Optional<IndexStatus> getStatusByRepoId(String repoId) {
        // First check active jobs
        var activeJobs = indexRepository.findActiveJobsByRepoId(repoId);
        if (!activeJobs.isEmpty()) {
            return Optional.of(activeJobs.getFirst());
        }

        // Check if the repo has index info (previously completed)
        var indexInfo = indexRepository.findInfoByRepoId(repoId);
        if (indexInfo.isPresent()) {
            // Synthesize a completed status from the index info
            var info = indexInfo.get();
            return Optional.of(IndexStatus.builder()
                    .jobId("latest")
                    .repoId(repoId)
                    .state(IndexState.COMPLETED)
                    .progress(100)
                    .filesProcessed(info.fileCount())
                    .message("Index up to date")
                    .build());
        }

        return Optional.empty();
    }

    /**
     * Get repository index info.
     */
    public Optional<IndexInfo> getIndexInfo(String repoId) {
        return indexRepository.findInfoByRepoId(repoId);
    }

    /**
     * Delete repository index.
     */
    public void deleteIndex(String repoId) {
        try {
            engineClient.deleteIndex(repoId);
        } catch (Exception e) {
            log.warn("Engine deleteIndex failed for repo={}, continuing with local cleanup: {}", repoId, e.getMessage());
        }
        indexRepository.deleteByRepoId(repoId);
        log.info("Index deleted: repo={}", repoId);
    }

    private void updateStatus(String jobId, IndexState state, int progress, String message) {
        indexRepository.updateStatus(jobId, state, progress, message);
    }

    @NotNull
    private String fetchRepository(@NotNull IndexRequest request) {
        // In a real implementation, this would:
        // 1. Clone or fetch the repository
        // 2. Checkout the appropriate commit
        // 3. Return the local path
        return "/tmp/repos/" + request.repoId().replace("/", "_");
    }
}
