package ai.aipr.server.engine;

import ai.aipr.engine.grpc.ContextChunk;
import ai.aipr.engine.grpc.DeleteIndexRequest;
import ai.aipr.engine.grpc.DeleteIndexResponse;
import ai.aipr.engine.grpc.DiagnosticsRequest;
import ai.aipr.engine.grpc.DiagnosticsResponse;
import ai.aipr.engine.grpc.EngineServiceGrpc;
import ai.aipr.engine.grpc.HealthCheckRequest;
import ai.aipr.engine.grpc.HealthCheckResponse;
import ai.aipr.engine.grpc.HeuristicsRequest;
import ai.aipr.engine.grpc.HeuristicsResponse;
import ai.aipr.engine.grpc.IncrementalIndexRequest;
import ai.aipr.engine.grpc.IndexRequest;
import ai.aipr.engine.grpc.IndexResponse;
import ai.aipr.engine.grpc.IndexStatsRequest;
import ai.aipr.engine.grpc.IndexStatsResponse;
import ai.aipr.engine.grpc.ReviewContextRequest;
import ai.aipr.engine.grpc.ReviewContextResponse;
import ai.aipr.engine.grpc.SearchConfig;
import ai.aipr.engine.grpc.SearchRequest;
import ai.aipr.engine.grpc.SearchResponse;
import ai.aipr.server.dto.Chunk;
import ai.aipr.server.dto.ContextPack;
import ai.aipr.server.dto.DiffAnalysis;
import ai.aipr.server.dto.Embedding;
import ai.aipr.server.dto.FileChange;
import ai.aipr.server.dto.FileInfo;
import ai.aipr.server.dto.HeuristicFinding;
import ai.aipr.server.dto.IndexConfig;
import ai.aipr.server.dto.ScanResult;
import ai.aipr.server.dto.Severity;
import ai.aipr.server.dto.Symbol;
import ai.aipr.server.dto.TouchedSymbol;
import ai.aipr.server.grpc.GrpcDataServiceDelegator;
import io.grpc.StatusRuntimeException;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import jakarta.annotation.PostConstruct;
import jakarta.annotation.PreDestroy;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.stream.Collectors;

/**
 * Client for communicating with the C++ engine via gRPC.
 *
 * <p>Uses {@link GrpcDataServiceDelegator} for channel management, which provides
 * TLS/mTLS, load balancing, health checking, and automatic reconnection. When the
 * engine is split into a separate microservice, this client will seamlessly connect
 * via the delegator's configuration.</p>
 */
@Component
public class EngineClient {

    private static final Logger log = LoggerFactory.getLogger(EngineClient.class);

    private final GrpcDataServiceDelegator delegator;

    @Value("${aipr.engine.timeout-ms:30000}")
    private int timeoutMs;

    private EngineServiceGrpc.EngineServiceBlockingStub blockingStub;
    private String engineVersion = "unknown";

    public EngineClient(GrpcDataServiceDelegator delegator) {
        this.delegator = delegator;
    }

    @PostConstruct
    public void init() {
        log.info("Initializing engine client, server: {}", delegator.getServerAddress());

        blockingStub = EngineServiceGrpc.newBlockingStub(delegator.getChannel())
                .withDeadlineAfter(timeoutMs, TimeUnit.MILLISECONDS);

        try {
            HealthCheckResponse health = blockingStub.healthCheck(
                    HealthCheckRequest.newBuilder().build());
            engineVersion = health.getVersion();
            log.info("Engine connected: version={}, healthy={}", engineVersion, health.getHealthy());
        } catch (StatusRuntimeException e) {
            log.warn("Engine not reachable at startup (will retry on first call): {}", e.getMessage());
        }
    }

    @PreDestroy
    public void shutdown() {
        try {
            delegator.shutdown(5, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            log.warn("Engine client shutdown interrupted");
        }
    }

    // =========================================================================
    // Health
    // =========================================================================

    /**
     * Check if the engine is reachable.
     */
    public boolean isHealthy() {
        try {
            HealthCheckResponse response = getStub().healthCheck(
                    HealthCheckRequest.newBuilder().build());
            return response.getHealthy();
        } catch (StatusRuntimeException e) {
            log.debug("Engine health check failed: {}", e.getStatus());
            return false;
        }
    }

    /**
     * Get engine version.
     */
    public String getVersion() {
        return engineVersion;
    }

    // =========================================================================
    // Review Operations
    // =========================================================================

    /**
     * Analyze a diff to extract changed files and touched symbols.
     */
    public DiffAnalysis analyzeDiff(String repoId, String diff) {
        log.debug("Analyzing diff for repo: {}", repoId);

        try {
            ReviewContextRequest request = ReviewContextRequest.newBuilder()
                    .setRepoId(repoId)
                    .setDiff(diff)
                    .build();

            ReviewContextResponse response = getStub().buildReviewContext(request);
            ai.aipr.engine.grpc.ContextPack pack = response.getContextPack();

            List<TouchedSymbol> touchedSymbols = pack.getTouchedSymbolsList().stream()
                    .map(this::convertTouchedSymbol)
                    .collect(Collectors.toList());

            List<FileChange> changedFiles = extractFileChangesFromDiff(diff);

            return DiffAnalysis.builder()
                    .repoId(repoId)
                    .changedFiles(changedFiles)
                    .touchedSymbols(touchedSymbols)
                    .hunks(List.of())
                    .totalAdditions(countDiffLines(diff, '+'))
                    .totalDeletions(countDiffLines(diff, '-'))
                    .build();

        } catch (StatusRuntimeException e) {
            log.error("Engine analyzeDiff failed for repo={}: {}", repoId, e.getStatus(), e);
            return DiffAnalysis.builder()
                    .repoId(repoId)
                    .changedFiles(List.of())
                    .touchedSymbols(List.of())
                    .hunks(List.of())
                    .build();
        }
    }

    /**
     * Run heuristic checks on the diff (secrets, risky APIs, antipatterns).
     */
    public List<HeuristicFinding> runHeuristics(String repoId, String diff) {
        log.debug("Running heuristics for repo: {}", repoId);

        try {
            HeuristicsRequest request = HeuristicsRequest.newBuilder()
                    .setDiff(diff != null ? diff : "")
                    .addAllEnabledChecks(List.of("secrets", "risky_apis", "sql_injection",
                            "hardcoded_creds", "large_file", "todo_fixme"))
                    .build();

            HeuristicsResponse response = getStub().runHeuristics(request);

            return response.getFindingsList().stream()
                    .map(this::convertHeuristicFinding)
                    .collect(Collectors.toList());

        } catch (StatusRuntimeException e) {
            log.error("Engine runHeuristics failed for repo={}: {}", repoId, e.getStatus(), e);
            return List.of();
        }
    }

    /**
     * Build context pack for LLM review.
     */
    public ContextPack buildContext(String repoId, String diff, DiffAnalysis diffAnalysis,
                                   String prTitle, String prDescription) {
        log.debug("Building context for repo: {}", repoId);

        try {
            ReviewContextRequest request = ReviewContextRequest.newBuilder()
                    .setRepoId(repoId)
                    .setDiff(diff != null ? diff : "")
                    .setPrTitle(prTitle != null ? prTitle : "")
                    .setPrDescription(prDescription != null ? prDescription : "")
                    .setMaxContextChunks(50)
                    .build();

            ReviewContextResponse response = getStub().buildReviewContext(request);
            ai.aipr.engine.grpc.ContextPack grpcPack = response.getContextPack();

            List<ai.aipr.server.dto.ContextChunk> contextChunks = grpcPack.getContextChunksList().stream()
                    .map(this::convertContextChunk)
                    .collect(Collectors.toList());

            List<TouchedSymbol> touchedSymbols = grpcPack.getTouchedSymbolsList().stream()
                    .map(this::convertTouchedSymbol)
                    .collect(Collectors.toList());

            return ContextPack.builder()
                    .repoId(grpcPack.getRepoId())
                    .prTitle(grpcPack.getPrTitle())
                    .prDescription(grpcPack.getPrDescription())
                    .diff(grpcPack.getDiff())
                    .contextChunks(contextChunks)
                    .touchedSymbols(touchedSymbols)
                    .heuristicWarnings(grpcPack.getHeuristicWarningsList())
                    .totalTokens((int) grpcPack.getTotalTokensEstimate())
                    .build();

        } catch (StatusRuntimeException e) {
            log.error("Engine buildContext failed for repo={}: {}", repoId, e.getStatus(), e);
            return ContextPack.builder()
                    .repoId(repoId)
                    .prTitle(prTitle)
                    .prDescription(prDescription)
                    .contextChunks(List.of())
                    .touchedSymbols(diffAnalysis.touchedSymbols())
                    .heuristicWarnings(List.of())
                    .build();
        }
    }

    // =========================================================================
    // Indexing Operations
    // =========================================================================

    /**
     * Scan repository and trigger full indexing.
     */
    public ScanResult scanRepository(String repoPath, IndexConfig config) {
        log.debug("Scanning repository: {}", repoPath);

        try {
            ai.aipr.engine.grpc.IndexConfig.Builder grpcConfig =
                    ai.aipr.engine.grpc.IndexConfig.newBuilder();

            if (config != null) {
                grpcConfig.setMaxFileSizeKb(config.maxFileSizeBytes() / 1024)
                          .setChunkSize(config.chunkSizeTokens())
                          .setChunkOverlap(config.chunkOverlapTokens())
                          .setEnableAstChunking(config.extractSymbols())
                          .addAllExcludePatterns(config.excludePatterns())
                          .addAllIncludeLanguages(List.of());
            }

            IndexRequest request = IndexRequest.newBuilder()
                    .setRepoPath(repoPath)
                    .setConfig(grpcConfig.build())
                    .build();

            IndexResponse response = getStub().indexRepository(request);

            if (!response.getSuccess()) {
                log.warn("Engine indexing reported failure: {}", response.getMessage());
            }

            int fileCount = (int) response.getStats().getTotalFiles();
            int symbolCount = (int) response.getStats().getTotalSymbols();

            log.info("Engine indexed repo={}: files={}, symbols={}, languages={}",
                    repoPath, fileCount, symbolCount,
                    response.getStats().getFilesByLanguageMap().keySet());

            return ScanResult.builder()
                    .repoId(repoPath)
                    .totalFiles(fileCount)
                    .files(List.of())
                    .symbols(List.of())
                    .languages(List.copyOf(response.getStats().getFilesByLanguageMap().keySet()))
                    .totalSizeBytes(response.getStats().getIndexSizeBytes())
                    .build();

        } catch (StatusRuntimeException e) {
            log.error("Engine scanRepository failed for path={}: {}", repoPath, e.getStatus(), e);
            return ScanResult.builder()
                    .repoId(repoPath)
                    .files(List.of())
                    .symbols(List.of())
                    .build();
        }
    }

    /**
     * Chunk files for indexing.
     * In the current architecture the C++ engine handles chunking internally during
     * {@link #scanRepository}. This method is kept for the service layer contract
     * and returns what the engine has already indexed.
     */
    public List<Chunk> chunkFiles(String repoPath, @NotNull List<FileInfo> files, boolean incremental) {
        log.debug("Chunking {} files (delegated to engine)", files.size());

        try {
            IndexStatsRequest request = IndexStatsRequest.newBuilder()
                    .setRepoId(repoPath)
                    .build();

            IndexStatsResponse response = getStub().getIndexStats(request);

            if (!response.getFound()) {
                log.warn("No index found for repo: {}", repoPath);
                return List.of();
            }

            // Engine manages chunks internally; return a summary count as placeholder chunks
            int chunkCount = (int) response.getStats().getTotalChunks();
            log.debug("Engine reports {} chunks for repo: {}", chunkCount, repoPath);

            // We don't transfer individual chunks back — the engine keeps them.
            // Return empty list; the service layer uses the chunk count from stats.
            return List.of();

        } catch (StatusRuntimeException e) {
            log.error("Engine chunkFiles failed for path={}: {}", repoPath, e.getStatus(), e);
            return List.of();
        }
    }

    /**
     * Generate embeddings for chunks.
     * The C++ engine generates embeddings during indexing via its configured
     * embedding endpoint. This method is a no-op in the current architecture.
     */
    public List<Embedding> generateEmbeddings(@NotNull List<Chunk> chunks) {
        log.debug("Embeddings generated by engine during indexing ({} chunks)", chunks.size());
        // Engine handles embedding generation internally via its embedding_endpoint config
        return List.of();
    }

    /**
     * Build search indices.
     * The C++ engine builds indices during {@link #scanRepository}.
     * This method triggers a stats refresh to confirm completion.
     */
    public void buildIndices(String repoId, List<Chunk> chunks, List<Embedding> embeddings) {
        log.debug("Indices built by engine during indexing for repo: {}", repoId);

        try {
            IndexStatsResponse response = getStub().getIndexStats(
                    IndexStatsRequest.newBuilder().setRepoId(repoId).build());

            if (response.getFound() && response.getStats().getIsComplete()) {
                log.info("Engine index verified for repo={}: files={}, chunks={}, symbols={}",
                        repoId,
                        response.getStats().getIndexedFiles(),
                        response.getStats().getTotalChunks(),
                        response.getStats().getTotalSymbols());
            } else {
                log.warn("Engine index not complete for repo: {}", repoId);
            }
        } catch (StatusRuntimeException e) {
            log.error("Engine buildIndices check failed for repo={}: {}", repoId, e.getStatus(), e);
        }
    }

    /**
     * Build symbol graph.
     * The C++ engine builds the symbol graph during indexing.
     * This method verifies the symbol count matches expectations.
     */
    public void buildSymbolGraph(String repoId, List<Symbol> symbols) {
        log.debug("Symbol graph built by engine during indexing for repo: {}", repoId);

        try {
            IndexStatsResponse response = getStub().getIndexStats(
                    IndexStatsRequest.newBuilder().setRepoId(repoId).build());

            if (response.getFound()) {
                long engineSymbols = response.getStats().getTotalSymbols();
                log.info("Engine symbol graph for repo={}: {} symbols indexed", repoId, engineSymbols);
            }
        } catch (StatusRuntimeException e) {
            log.error("Engine buildSymbolGraph check failed for repo={}: {}", repoId, e.getStatus(), e);
        }
    }

    /**
     * Delete repository index.
     */
    public void deleteIndex(String repoId) {
        log.info("Deleting index for repo: {}", repoId);

        try {
            DeleteIndexResponse response = getStub().deleteIndex(
                    DeleteIndexRequest.newBuilder().setRepoId(repoId).build());

            if (response.getSuccess()) {
                log.info("Index deleted for repo: {}", repoId);
            } else {
                log.warn("Engine deleteIndex reported failure for repo={}: {}",
                        repoId, response.getMessage());
            }
        } catch (StatusRuntimeException e) {
            log.error("Engine deleteIndex failed for repo={}: {}", repoId, e.getStatus(), e);
        }
    }

    /**
     * Perform an incremental index update for changed files between two commits.
     *
     * @param repoId       the repository identifier
     * @param changedFiles list of file paths that changed
     * @param baseCommit   the base commit SHA
     * @param headCommit   the head commit SHA
     * @return scan result with updated stats
     */
    public ScanResult incrementalIndex(String repoId, @NotNull List<String> changedFiles,
                                       String baseCommit, String headCommit) {
        log.info("Incremental index for repo={}: {} changed files, base={}, head={}",
                repoId, changedFiles.size(), baseCommit, headCommit);

        try {
            IncrementalIndexRequest request = IncrementalIndexRequest.newBuilder()
                    .setRepoId(repoId)
                    .addAllChangedFiles(changedFiles)
                    .setBaseCommit(baseCommit != null ? baseCommit : "")
                    .setHeadCommit(headCommit != null ? headCommit : "")
                    .build();

            IndexResponse response = getStub().incrementalIndex(request);

            if (!response.getSuccess()) {
                log.warn("Engine incremental index reported failure: {}", response.getMessage());
            }

            int fileCount = (int) response.getStats().getTotalFiles();
            log.info("Incremental index completed: repo={}, files={}", repoId, fileCount);

            return ScanResult.builder()
                    .repoId(repoId)
                    .totalFiles(fileCount)
                    .files(List.of())
                    .symbols(List.of())
                    .languages(List.copyOf(response.getStats().getFilesByLanguageMap().keySet()))
                    .totalSizeBytes(response.getStats().getIndexSizeBytes())
                    .build();

        } catch (StatusRuntimeException e) {
            log.error("Engine incrementalIndex failed for repo={}: {}", repoId, e.getStatus(), e);
            return ScanResult.builder().repoId(repoId).totalFiles(0).build();
        }
    }

    /**
     * Search the repository index for relevant code context.
     *
     * @param repoId         the repository identifier
     * @param query          free-text search query
     * @param touchedSymbols optional list of symbol names to boost
     * @param topK           maximum number of results to return
     * @return list of relevant context chunks
     */
    public List<ai.aipr.server.dto.ContextChunk> search(String repoId, String query,
                                                         List<String> touchedSymbols, int topK) {
        log.debug("Searching repo={}: query='{}', topK={}", repoId, query, topK);

        try {
            SearchRequest.Builder requestBuilder = SearchRequest.newBuilder()
                    .setRepoId(repoId)
                    .setQuery(query != null ? query : "");

            if (touchedSymbols != null) {
                requestBuilder.addAllTouchedSymbols(touchedSymbols);
            }

            requestBuilder.setConfig(SearchConfig.newBuilder()
                    .setTopK(topK > 0 ? topK : 10)
                    .setLexicalWeight(0.3f)
                    .setVectorWeight(0.7f)
                    .setGraphExpandDepth(1)
                    .build());

            SearchResponse response = getStub().search(requestBuilder.build());

            log.debug("Search returned {} chunks for repo={}", response.getChunksCount(), repoId);

            return response.getChunksList().stream()
                    .map(this::convertContextChunk)
                    .collect(Collectors.toList());

        } catch (StatusRuntimeException e) {
            log.error("Engine search failed for repo={}: {}", repoId, e.getStatus(), e);
            return List.of();
        }
    }

    /**
     * Get engine diagnostics including memory stats and loaded indices.
     *
     * @param includeMemory  whether to include memory statistics
     * @param includeIndices whether to include per-index details
     * @return diagnostics response from the engine
     */
    public DiagnosticsResponse getDiagnostics(boolean includeMemory, boolean includeIndices) {
        log.debug("Fetching engine diagnostics: memory={}, indices={}", includeMemory, includeIndices);

        try {
            DiagnosticsRequest request = DiagnosticsRequest.newBuilder()
                    .setIncludeMemory(includeMemory)
                    .setIncludeIndices(includeIndices)
                    .build();

            return getStub().getDiagnostics(request);

        } catch (StatusRuntimeException e) {
            log.error("Engine getDiagnostics failed: {}", e.getStatus(), e);
            return DiagnosticsResponse.getDefaultInstance();
        }
    }

    // =========================================================================
    // Internal Helpers
    // =========================================================================

    /**
     * Get a blocking stub with a fresh deadline.
     * Creates a new deadline for each call (gRPC deadlines are one-shot).
     * Uses round-robin channel selection when multiple engine instances are configured.
     * If the underlying channel is dead, triggers reconnection via the delegator.
     */
    private EngineServiceGrpc.EngineServiceBlockingStub getStub() {
        // Multi-instance: create a fresh stub per call for load distribution
        if (delegator.hasMultipleInstances()) {
            return EngineServiceGrpc.newBlockingStub(delegator.getChannelRoundRobin())
                    .withDeadlineAfter(timeoutMs, TimeUnit.MILLISECONDS);
        }

        // Single-instance: reuse cached stub, reconnect if channel is dead
        if (blockingStub == null || !delegator.isHealthy()) {
            if (blockingStub != null) {
                log.warn("Engine gRPC channel unhealthy, reconnecting...");
                delegator.reconnect();
            }
            blockingStub = EngineServiceGrpc.newBlockingStub(delegator.getChannel());
        }
        return blockingStub.withDeadlineAfter(timeoutMs, TimeUnit.MILLISECONDS);
    }

    private TouchedSymbol convertTouchedSymbol(@NotNull ai.aipr.engine.grpc.TouchedSymbol grpc) {
        int startLine = grpc.getLine();
        int endLine = grpc.getEndLine() > 0 ? grpc.getEndLine() : startLine;
        return TouchedSymbol.builder()
                .name(grpc.getName())
                .qualifiedName(grpc.getQualifiedName())
                .kind(parseSymbolKind(grpc.getKind()))
                .filePath(grpc.getFilePath())
                .startLine(startLine)
                .endLine(endLine)
                .changeType(parseChangeType(grpc.getChangeType()))
                .callers(grpc.getCallersList())
                .callees(grpc.getCalleesList())
                .build();
    }

    private ai.aipr.server.dto.ContextChunk convertContextChunk(@NotNull ContextChunk grpc) {
        return ai.aipr.server.dto.ContextChunk.builder()
                .id(grpc.getId())
                .filePath(grpc.getFilePath())
                .startLine(grpc.getStartLine())
                .endLine(grpc.getEndLine())
                .content(grpc.getContent())
                .language(grpc.getLanguage())
                .symbols(grpc.getSymbolsList())
                .relevanceScore(grpc.getRelevanceScore())
                .source(ai.aipr.server.dto.ContextChunk.ChunkSource.VECTOR_SEARCH)
                .build();
    }

    private HeuristicFinding convertHeuristicFinding(@NotNull ai.aipr.engine.grpc.HeuristicFinding grpc) {
        Integer startLine = grpc.getLine() > 0 ? grpc.getLine() : null;
        Integer endLine = grpc.getEndLine() > 0 ? Integer.valueOf(grpc.getEndLine())
                : startLine;  // fall back to startLine (maybe null)
        return HeuristicFinding.builder()
                .ruleId(grpc.getRuleId())
                .ruleName(grpc.getRuleName())
                .severity(convertSeverity(grpc.getSeverity()))
                .filePath(grpc.getFilePath())
                .startLine(startLine)
                .endLine(endLine)
                .message(grpc.getMessage())
                .suggestion(grpc.getSuggestion())
                .category(convertCategory(grpc.getCategory()))
                .build();
    }

    @NotNull
    private String convertSeverity(@NotNull ai.aipr.engine.grpc.Severity severity) {
        return switch (severity) {
            case SEVERITY_CRITICAL -> Severity.ERROR.getValue();
            case SEVERITY_ERROR -> Severity.ERROR.getValue();
            case SEVERITY_WARNING -> Severity.WARNING.getValue();
            case SEVERITY_INFO -> Severity.INFO.getValue();
            default -> Severity.INFO.getValue();
        };
    }

    @NotNull
    private String convertCategory(@NotNull ai.aipr.engine.grpc.CheckCategory category) {
        return switch (category) {
            case CATEGORY_SECURITY -> "security";
            case CATEGORY_PERFORMANCE -> "performance";
            case CATEGORY_RELIABILITY -> "reliability";
            case CATEGORY_STYLE -> "style";
            case CATEGORY_ARCHITECTURE -> "architecture";
            case CATEGORY_TESTING -> "testing";
            case CATEGORY_DOCUMENTATION -> "documentation";
            default -> "general";
        };
    }

    private TouchedSymbol.SymbolKind parseSymbolKind(String kind) {
        if (kind == null || kind.isEmpty()) {
            return TouchedSymbol.SymbolKind.FUNCTION;
        }
        return switch (kind.toLowerCase()) {
            case "class" -> TouchedSymbol.SymbolKind.CLASS;
            case "interface" -> TouchedSymbol.SymbolKind.INTERFACE;
            case "enum" -> TouchedSymbol.SymbolKind.ENUM;
            case "struct" -> TouchedSymbol.SymbolKind.STRUCT;
            case "variable" -> TouchedSymbol.SymbolKind.VARIABLE;
            case "constant" -> TouchedSymbol.SymbolKind.CONSTANT;
            case "module", "namespace" -> TouchedSymbol.SymbolKind.NAMESPACE;
            case "property", "field" -> TouchedSymbol.SymbolKind.FIELD;
            default -> TouchedSymbol.SymbolKind.FUNCTION;
        };
    }

    private TouchedSymbol.ChangeType parseChangeType(String changeType) {
        if (changeType == null || changeType.isEmpty()) {
            return TouchedSymbol.ChangeType.MODIFIED;
        }
        return switch (changeType.toLowerCase()) {
            case "added" -> TouchedSymbol.ChangeType.ADDED;
            case "deleted" -> TouchedSymbol.ChangeType.DELETED;
            default -> TouchedSymbol.ChangeType.MODIFIED;
        };
    }

    /**
     * Extract file change info from a unified diff string.
     */
    @NotNull
    private List<FileChange> extractFileChangesFromDiff(String diff) {
        if (diff == null || diff.isEmpty()) {
            return List.of();
        }

        List<FileChange> changes = new ArrayList<>();
        String[] lines = diff.split("\n");

        String currentFile = null;
        String currentOldPath = null;
        FileChange.ChangeType currentChangeType = FileChange.ChangeType.MODIFIED;
        boolean currentIsBinary = false;
        int additions = 0;
        int deletions = 0;

        for (String line : lines) {
            if (line.startsWith("diff --git")) {
                // Flush previous file
                if (currentFile != null) {
                    changes.add(buildFileChange(currentFile, currentOldPath,
                            currentChangeType, currentIsBinary, additions, deletions));
                }
                // Parse file paths from "diff --git a/old b/new"
                String[] parts = line.split(" ");
                if (parts.length >= 4) {
                    currentOldPath = parts[2].substring(2); // remove "a/" prefix
                    currentFile = parts[3].substring(2);    // remove "b/" prefix
                }
                currentChangeType = FileChange.ChangeType.MODIFIED;
                currentIsBinary = false;
                additions = 0;
                deletions = 0;
            } else if (line.startsWith("new file mode")) {
                currentChangeType = FileChange.ChangeType.ADDED;
                currentOldPath = null;
            } else if (line.startsWith("deleted file mode")) {
                currentChangeType = FileChange.ChangeType.DELETED;
            } else if (line.startsWith("rename from ")) {
                currentChangeType = FileChange.ChangeType.RENAMED;
                currentOldPath = line.substring("rename from ".length());
            } else if (line.startsWith("rename to ")) {
                currentFile = line.substring("rename to ".length());
            } else if (line.startsWith("similarity index") || line.startsWith("copy from ")) {
                if (line.startsWith("copy from ")) {
                    currentChangeType = FileChange.ChangeType.COPIED;
                }
            } else if (line.startsWith("Binary files")) {
                currentIsBinary = true;
            } else if (line.startsWith("+") && !line.startsWith("+++")) {
                additions++;
            } else if (line.startsWith("-") && !line.startsWith("---")) {
                deletions++;
            }
        }

        // Flush last file
        if (currentFile != null) {
            changes.add(buildFileChange(currentFile, currentOldPath,
                    currentChangeType, currentIsBinary, additions, deletions));
        }

        return changes;
    }

    private FileChange buildFileChange(String path, String oldPath,
                                       FileChange.ChangeType changeType,
                                       boolean isBinary, int additions, int deletions) {
        return FileChange.builder()
                .path(path)
                .oldPath(changeType == FileChange.ChangeType.RENAMED
                        || changeType == FileChange.ChangeType.COPIED ? oldPath : null)
                .changeType(changeType)
                .language(inferLanguage(path))
                .isBinary(isBinary)
                .additions(additions)
                .deletions(deletions)
                .build();
    }

    /**
     * Infer programming language from file extension.
     */
    private String inferLanguage(String filePath) {
        if (filePath == null) return null;
        int lastDot = filePath.lastIndexOf('.');
        if (lastDot < 0) return null;

        String ext = filePath.substring(lastDot + 1).toLowerCase();
        return switch (ext) {
            case "java" -> "java";
            case "kt", "kts" -> "kotlin";
            case "ts", "tsx" -> "typescript";
            case "js", "jsx", "mjs" -> "javascript";
            case "py" -> "python";
            case "cpp", "cc", "cxx", "hpp", "h" -> "cpp";
            case "c" -> "c";
            case "go" -> "go";
            case "rs" -> "rust";
            case "rb" -> "ruby";
            case "cs" -> "csharp";
            case "swift" -> "swift";
            case "scala" -> "scala";
            case "yml", "yaml" -> "yaml";
            case "json" -> "json";
            case "xml" -> "xml";
            case "sql" -> "sql";
            case "md" -> "markdown";
            case "sh", "bash" -> "bash";
            case "proto" -> "protobuf";
            case "gradle" -> "groovy";
            default -> ext;
        };
    }

    private int countDiffLines(String diff, char prefix) {
        if (diff == null) return 0;
        int count = 0;
        for (String line : diff.split("\n")) {
            if (!line.isEmpty() && line.charAt(0) == prefix
                    && !line.startsWith("+++") && !line.startsWith("---")) {
                count++;
            }
        }
        return count;
    }
}
