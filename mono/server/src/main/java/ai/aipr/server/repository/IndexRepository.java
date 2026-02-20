package ai.aipr.server.repository;

import ai.aipr.server.dto.IndexInfo;
import ai.aipr.server.dto.IndexState;
import ai.aipr.server.dto.IndexStatus;
import org.springframework.stereotype.Repository;

import java.util.List;
import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Repository for storing indexing status and information.
 * Uses in-memory storage for now, can be replaced with Redis/DB.
 */
@Repository
public class IndexRepository {
    
    private final ConcurrentHashMap<String, IndexStatus> statusMap = new ConcurrentHashMap<>();
    private final ConcurrentHashMap<String, IndexInfo> infoMap = new ConcurrentHashMap<>();
    
    /**
     * Save or update indexing status.
     */
    public void saveStatus(IndexStatus status) {
        statusMap.put(status.jobId(), status);
    }
    
    /**
     * Update status fields for a job.
     */
    public void updateStatus(String jobId, IndexState state, int progress, String message) {
        var existing = statusMap.get(jobId);
        if (existing != null) {
            var updated = IndexStatus.builder()
                    .jobId(existing.jobId())
                    .repoId(existing.repoId())
                    .state(state)
                    .progress(progress)
                    .message(message)
                    .startTime(existing.startTime())
                    .endTime(existing.endTime())
                    .stats(existing.stats())
                    .errors(existing.errors())
                    .build();
            statusMap.put(jobId, updated);
        }
    }
    
    /**
     * Get indexing status by job ID.
     */
    public Optional<IndexStatus> findStatusByJobId(String jobId) {
        return Optional.ofNullable(statusMap.get(jobId));
    }
    
    /**
     * Alias for findStatusByJobId.
     */
    public Optional<IndexStatus> findStatusById(String jobId) {
        return findStatusByJobId(jobId);
    }
    
    /**
     * Get all active indexing jobs for a repository.
     */
    public List<IndexStatus> findActiveJobsByRepoId(String repoId) {
        return statusMap.values().stream()
                .filter(s -> s.repoId().equals(repoId))
                .filter(s -> s.state() == IndexState.RUNNING || s.state() == IndexState.PENDING)
                .toList();
    }
    
    /**
     * Delete indexing status by job ID.
     */
    public void deleteStatus(String jobId) {
        statusMap.remove(jobId);
    }
    
    /**
     * Save or update index information.
     */
    public void saveInfo(IndexInfo info) {
        infoMap.put(info.repoId(), info);
    }
    
    /**
     * Get index information by repository ID.
     */
    public Optional<IndexInfo> findInfoByRepoId(String repoId) {
        return Optional.ofNullable(infoMap.get(repoId));
    }
    
    /**
     * Check if a repository is indexed.
     */
    public boolean isIndexed(String repoId) {
        return infoMap.containsKey(repoId);
    }
    
    /**
     * Delete index information by repository ID.
     */
    public void deleteInfo(String repoId) {
        infoMap.remove(repoId);
    }
    
    /**
     * Delete all data for a repository.
     */
    public void deleteByRepoId(String repoId) {
        // Remove index info
        infoMap.remove(repoId);
        
        // Remove all status entries for this repo
        statusMap.entrySet().removeIf(e -> e.getValue().repoId().equals(repoId));
    }
    
    /**
     * List all indexed repositories.
     */
    public List<IndexInfo> listAllIndexes() {
        return List.copyOf(infoMap.values());
    }
    
    /**
     * Clean up completed/failed jobs older than a certain age.
     */
    public int cleanupOldJobs(long maxAgeMs) {
        long cutoff = System.currentTimeMillis() - maxAgeMs;
        int removed = 0;
        
        var iterator = statusMap.entrySet().iterator();
        while (iterator.hasNext()) {
            var entry = iterator.next();
            var status = entry.getValue();
            
            if ((status.state() == IndexState.COMPLETED || status.state() == IndexState.FAILED) 
                    && status.endTime() != null 
                    && status.endTime().toEpochMilli() < cutoff) {
                iterator.remove();
                removed++;
            }
        }
        
        return removed;
    }
}
