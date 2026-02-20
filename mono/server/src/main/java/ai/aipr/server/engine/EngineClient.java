package ai.aipr.server.engine;

import ai.aipr.server.dto.*;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import jakarta.annotation.PostConstruct;
import jakarta.annotation.PreDestroy;
import java.util.List;
import java.util.concurrent.TimeUnit;

/**
 * Client for communicating with the C++ engine via gRPC.
 */
@Component
public class EngineClient {
    
    private static final Logger log = LoggerFactory.getLogger(EngineClient.class);
    
    @Value("${aipr.engine.host}")
    private String engineHost;
    
    @Value("${aipr.engine.port}")
    private int enginePort;
    
    @Value("${aipr.engine.timeout-ms}")
    private int timeoutMs;
    
    private ManagedChannel channel;
    private String engineVersion = "0.1.0";
    
    @PostConstruct
    public void init() {
        log.info("Connecting to engine at {}:{}", engineHost, enginePort);
        
        channel = ManagedChannelBuilder.forAddress(engineHost, enginePort)
                .usePlaintext()  // TODO: Enable TLS in production
                .build();
        
        // TODO: Create gRPC stubs when proto is generated
        // engineStub = EngineServiceGrpc.newBlockingStub(channel);
        
        log.info("Engine client initialized");
    }
    
    @PreDestroy
    public void shutdown() {
        if (channel != null) {
            try {
                channel.shutdown().awaitTermination(5, TimeUnit.SECONDS);
            } catch (InterruptedException e) {
                channel.shutdownNow();
            }
        }
    }
    
    /**
     * Analyze a diff to extract changed files and touched symbols.
     */
    public DiffAnalysis analyzeDiff(String repoId, String diff) {
        log.debug("Analyzing diff for repo: {}", repoId);
        
        // TODO: Call engine via gRPC
        // var request = AnalyzeDiffRequest.newBuilder()
        //         .setRepoId(repoId)
        //         .setDiff(diff)
        //         .build();
        // return engineStub.analyzeDiff(request);
        
        // Stub implementation
        return DiffAnalysis.builder()
                .repoId(repoId)
                .changedFiles(List.of())
                .touchedSymbols(List.of())
                .hunks(List.of())
                .totalAdditions(0)
                .totalDeletions(0)
                .build();
    }
    
    /**
     * Run heuristic checks on the diff.
     */
    public List<HeuristicFinding> runHeuristics(String repoId, DiffAnalysis diff) {
        log.debug("Running heuristics for repo: {}", repoId);
        
        // TODO: Call engine via gRPC
        return List.of();
    }
    
    /**
     * Build context pack for LLM review.
     */
    public ContextPack buildContext(
            String repoId,
            DiffAnalysis diff,
            String prTitle,
            String prDescription
    ) {
        log.debug("Building context for repo: {}", repoId);
        
        // TODO: Call engine via gRPC for:
        // 1. Search for relevant chunks
        // 2. Get symbol definitions
        // 3. Build context pack
        
        return ContextPack.builder()
                .repoId(repoId)
                .prTitle(prTitle)
                .prDescription(prDescription)
                .diff(diff)
                .chunks(List.of())
                .touchedSymbols(diff.touchedSymbols())
                .heuristicWarnings(List.of())
                .build();
    }
    
    /**
     * Scan repository for files.
     */
    public ScanResult scanRepository(String repoPath, IndexConfig config) {
        log.debug("Scanning repository: {}", repoPath);
        
        // TODO: Call engine via gRPC
        return ScanResult.builder()
                .files(List.of())
                .symbols(List.of())
                .build();
    }
    
    /**
     * Chunk files for indexing.
     */
    public List<Chunk> chunkFiles(String repoPath, List<FileInfo> files, boolean incremental) {
        log.debug("Chunking {} files", files.size());
        
        // TODO: Call engine via gRPC
        return List.of();
    }
    
    /**
     * Generate embeddings for chunks.
     */
    public List<Embedding> generateEmbeddings(List<Chunk> chunks) {
        log.debug("Generating embeddings for {} chunks", chunks.size());
        
        // TODO: Call embedding API
        return List.of();
    }
    
    /**
     * Build search indices.
     */
    public void buildIndices(String repoId, List<Chunk> chunks, List<Embedding> embeddings) {
        log.debug("Building indices for repo: {}", repoId);
        
        // TODO: Call engine via gRPC
    }
    
    /**
     * Build symbol graph.
     */
    public void buildSymbolGraph(String repoId, List<Symbol> symbols) {
        log.debug("Building symbol graph for repo: {}", repoId);
        
        // TODO: Call engine via gRPC
    }
    
    /**
     * Delete repository index.
     */
    public void deleteIndex(String repoId) {
        log.debug("Deleting index for repo: {}", repoId);
        
        // TODO: Call engine via gRPC
    }
    
    /**
     * Get engine version.
     */
    public String getVersion() {
        return engineVersion;
    }
}
