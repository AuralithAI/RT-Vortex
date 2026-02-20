package ai.aipr.server.repository;

import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.model.ReviewFilter;
import ai.aipr.server.model.PageRequest;
import ai.aipr.server.model.PagedResult;
import org.springframework.stereotype.Repository;

import java.util.List;
import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;
import java.util.stream.Stream;

/**
 * Repository for storing review data.
 * Uses in-memory storage for now, can be replaced with Redis/DB.
 */
@Repository
public class ReviewRepository {
    
    private final ConcurrentHashMap<String, ReviewResponse> reviewMap = new ConcurrentHashMap<>();
    
    /**
     * Save a review response.
     */
    public void save(ReviewResponse review) {
        reviewMap.put(review.reviewId(), review);
    }
    
    /**
     * Find a review by ID.
     */
    public Optional<ReviewResponse> findById(String reviewId) {
        return Optional.ofNullable(reviewMap.get(reviewId));
    }
    
    /**
     * Find all reviews for a repository.
     */
    public List<ReviewResponse> findByRepoId(String repoId) {
        return reviewMap.values().stream()
                .filter(r -> r.repoId().equals(repoId))
                .toList();
    }
    
    /**
     * Find reviews for a repository with pagination.
     */
    public List<ReviewResponse> findByRepoId(String repoId, int page, int size) {
        return reviewMap.values().stream()
                .filter(r -> r.repoId().equals(repoId))
                .skip((long) page * size)
                .limit(size)
                .toList();
    }
    
    /**
     * Find reviews for a specific PR.
     */
    public List<ReviewResponse> findByRepoIdAndPrNumber(String repoId, int prNumber) {
        return reviewMap.values().stream()
                .filter(r -> r.repoId().equals(repoId) && r.prNumber() == prNumber)
                .toList();
    }
    
    /**
     * Find reviews matching a filter.
     */
    public PagedResult<ReviewResponse> findByFilter(ReviewFilter filter, PageRequest pageRequest) {
        Stream<ReviewResponse> stream = reviewMap.values().stream();
        
        if (filter.repoId() != null) {
            stream = stream.filter(r -> r.repoId().equals(filter.repoId()));
        }
        
        if (filter.prNumber() != null) {
            stream = stream.filter(r -> r.prNumber() == filter.prNumber());
        }
        
        if (filter.status() != null) {
            stream = stream.filter(r -> r.status().equals(filter.status()));
        }
        
        List<ReviewResponse> allMatching = stream.toList();
        int total = allMatching.size();
        
        List<ReviewResponse> page = allMatching.stream()
                .skip((long) pageRequest.page() * pageRequest.size())
                .limit(pageRequest.size())
                .toList();
        
        return new PagedResult<>(page, total, pageRequest.page(), pageRequest.size());
    }
    
    /**
     * Delete a review by ID.
     */
    public void deleteById(String reviewId) {
        reviewMap.remove(reviewId);
    }
    
    /**
     * Delete all reviews for a repository.
     */
    public int deleteByRepoId(String repoId) {
        List<String> toDelete = reviewMap.entrySet().stream()
                .filter(e -> e.getValue().repoId().equals(repoId))
                .map(e -> e.getKey())
                .toList();
        
        toDelete.forEach(reviewMap::remove);
        return toDelete.size();
    }
    
    /**
     * Count reviews for a repository.
     */
    public long countByRepoId(String repoId) {
        return reviewMap.values().stream()
                .filter(r -> r.repoId().equals(repoId))
                .count();
    }
}
