package ai.aipr.server.repository;

import ai.aipr.server.model.UserInfo;
import org.springframework.stereotype.Repository;

import java.util.List;
import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Repository for users.
 */
@Repository
public class UserRepository {
    
    private final ConcurrentHashMap<String, UserInfo> userMap = new ConcurrentHashMap<>();
    private final ConcurrentHashMap<String, String> emailToIdMap = new ConcurrentHashMap<>();
    
    /**
     * Save a user.
     */
    public void save(UserInfo user) {
        userMap.put(user.id(), user);
        if (user.email() != null) {
            emailToIdMap.put(user.email().toLowerCase(), user.id());
        }
    }
    
    /**
     * Find a user by ID.
     */
    public Optional<UserInfo> findById(String userId) {
        return Optional.ofNullable(userMap.get(userId));
    }
    
    /**
     * Find a user by email.
     */
    public Optional<UserInfo> findByEmail(String email) {
        String userId = emailToIdMap.get(email.toLowerCase());
        if (userId == null) {
            return Optional.empty();
        }
        return findById(userId);
    }
    
    /**
     * Check if a user exists.
     */
    public boolean existsById(String userId) {
        return userMap.containsKey(userId);
    }
    
    /**
     * Check if an email is already registered.
     */
    public boolean existsByEmail(String email) {
        return emailToIdMap.containsKey(email.toLowerCase());
    }
    
    /**
     * Delete a user by ID.
     */
    public void deleteById(String userId) {
        var user = userMap.remove(userId);
        if (user != null && user.email() != null) {
            emailToIdMap.remove(user.email().toLowerCase());
        }
    }
    
    /**
     * List all users.
     */
    public List<UserInfo> findAll() {
        return List.copyOf(userMap.values());
    }
    
    /**
     * Count total users.
     */
    public long count() {
        return userMap.size();
    }
}
