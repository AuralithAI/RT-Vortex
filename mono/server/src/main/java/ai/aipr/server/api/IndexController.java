package ai.aipr.server.api;

import ai.aipr.server.dto.IndexRequest;
import ai.aipr.server.dto.IndexStatus;
import ai.aipr.server.service.IndexingService;
import jakarta.validation.Valid;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.util.concurrent.CompletableFuture;

/**
 * REST API for repository indexing.
 */
@RestController
@RequestMapping("/api/v1/index")
public class IndexController {
    
    private static final Logger log = LoggerFactory.getLogger(IndexController.class);
    
    private final IndexingService indexingService;
    
    public IndexController(IndexingService indexingService) {
        this.indexingService = indexingService;
    }
    
    /**
     * Trigger full repository indexing.
     */
    @PostMapping("/full")
    public CompletableFuture<ResponseEntity<IndexStatus>> indexFull(
            @Valid @RequestBody IndexRequest request
    ) {
        log.info("Full indexing requested: repo={}", request.repoId());
        
        return indexingService.indexRepository(request, false)
                .thenApply(status -> ResponseEntity.accepted().body(status));
    }
    
    /**
     * Trigger incremental repository indexing.
     */
    @PostMapping("/incremental")
    public CompletableFuture<ResponseEntity<IndexStatus>> indexIncremental(
            @Valid @RequestBody IndexRequest request
    ) {
        log.info("Incremental indexing requested: repo={}, since={}", 
                request.repoId(), request.sinceCommit());
        
        return indexingService.indexRepository(request, true)
                .thenApply(status -> ResponseEntity.accepted().body(status));
    }
    
    /**
     * Get indexing status.
     */
    @GetMapping("/status/{jobId}")
    public ResponseEntity<IndexStatus> getStatus(@PathVariable String jobId) {
        return indexingService.getStatus(jobId)
                .map(ResponseEntity::ok)
                .orElse(ResponseEntity.notFound().build());
    }
    
    /**
     * Get repository index info.
     */
    @GetMapping("/info/{repoId}")
    public ResponseEntity<?> getIndexInfo(@PathVariable String repoId) {
        return indexingService.getIndexInfo(repoId)
                .map(ResponseEntity::ok)
                .orElse(ResponseEntity.notFound().build());
    }
    
    /**
     * Delete repository index.
     */
    @DeleteMapping("/{repoId}")
    public ResponseEntity<Void> deleteIndex(@PathVariable String repoId) {
        log.info("Deleting index: repo={}", repoId);
        indexingService.deleteIndex(repoId);
        return ResponseEntity.noContent().build();
    }
}
