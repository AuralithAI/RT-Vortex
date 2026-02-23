package ai.aipr.server.api;

import ai.aipr.server.repository.UserRepository;
import ai.aipr.server.service.RateLimiterService;
import ai.aipr.server.session.SessionManager;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.ObjectProvider;
import org.springframework.http.ResponseEntity;
import org.springframework.lang.Nullable;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PutMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Administrative endpoints for user management, rate-limit control,
 * session management, and subscription tier changes.
 *
 * <p>All endpoints require admin-level authentication (enforced by
 * Spring Security configuration).</p>
 */
@RestController
@RequestMapping("/api/v1/admin")
public class AdminController {

    private static final Logger log = LoggerFactory.getLogger(AdminController.class);

    private final UserRepository userRepository;
    private final SessionManager sessionManager;
    @Nullable private final RateLimiterService rateLimiterService;

    public AdminController(UserRepository userRepository,
                           SessionManager sessionManager,
                           @NotNull ObjectProvider<RateLimiterService> rateLimiterServiceProvider) {
        this.userRepository = userRepository;
        this.sessionManager = sessionManager;
        this.rateLimiterService = rateLimiterServiceProvider.getIfAvailable();
    }

    // =====================================================================
    // Subscription Tier
    // =====================================================================

    @PutMapping("/users/{userId}/tier")
    public ResponseEntity<Map<String, Object>> updateUserTier(
        @PathVariable String userId,
        @NotNull @RequestParam String tier,
        @RequestParam(defaultValue = "admin") String changedBy,
        @RequestParam(required = false) String reason) {

        String normalizedTier = tier.toUpperCase();
        if (!normalizedTier.matches("FREE|PRO|ENTERPRISE")) {
            return ResponseEntity.badRequest().body(Map.of(
                "error", "Invalid tier. Must be FREE, PRO, or ENTERPRISE"));
        }

        userRepository.updateTier(userId, normalizedTier, changedBy, reason);
        log.info("Updated tier for user {} to {} by {}", userId, normalizedTier, changedBy);

        Map<String, Object> response = new LinkedHashMap<>();
        response.put("userId", userId);
        response.put("tier", normalizedTier);
        response.put("changedBy", changedBy);
        return ResponseEntity.ok(response);
    }

    @GetMapping("/users/{userId}/tier")
    public ResponseEntity<Map<String, Object>> getUserTier(@PathVariable String userId) {
        String tier = userRepository.findTierByUserId(userId);
        return ResponseEntity.ok(Map.of("userId", userId, "tier", tier));
    }

    // =====================================================================
    // Session Management
    // =====================================================================

    @DeleteMapping("/sessions/{sessionToken}")
    public ResponseEntity<Map<String, Object>> revokeSession(@PathVariable String sessionToken) {
        sessionManager.revokeSession(sessionToken);
        return ResponseEntity.ok(Map.of("status", "revoked"));
    }

    @DeleteMapping("/users/{userId}/sessions")
    public ResponseEntity<Map<String, Object>> revokeAllUserSessions(@PathVariable String userId) {
        int count = sessionManager.revokeAllUserSessions(userId);
        return ResponseEntity.ok(Map.of("userId", userId, "sessionsRevoked", count));
    }

    // =====================================================================
    // Rate Limiting
    // =====================================================================

    @DeleteMapping("/users/{userId}/rate-limit")
    public ResponseEntity<Map<String, Object>> resetRateLimit(@PathVariable String userId) {
        if (rateLimiterService == null) {
            return ResponseEntity.badRequest().body(Map.of("error", "Redis not configured"));
        }
        rateLimiterService.resetRateLimit(userId);
        return ResponseEntity.ok(Map.of("userId", userId, "status", "rate_limit_reset"));
    }

    @GetMapping("/users/{userId}/rate-limit")
    public ResponseEntity<Map<String, Object>> getRemainingTokens(
            @PathVariable String userId,
            @RequestParam(defaultValue = "FREE") String tier) {
        if (rateLimiterService == null) {
            return ResponseEntity.badRequest().body(Map.of("error", "Redis not configured"));
        }
        RateLimiterService.Tier t;
        try {
            t = RateLimiterService.Tier.valueOf(tier.toUpperCase());
        } catch (IllegalArgumentException e) {
            return ResponseEntity.badRequest().body(Map.of("error", "Invalid tier"));
        }

        int remaining = rateLimiterService.getRemainingTokens(userId, t);
        Map<String, Object> response = new LinkedHashMap<>();
        response.put("userId", userId);
        response.put("tier", t.name());
        response.put("remainingTokens", remaining);
        response.put("capacity", t.getCapacity());
        return ResponseEntity.ok(response);
    }
}

