package ai.aipr.server.interceptor;

import ai.aipr.server.config.RedisConfig;
import ai.aipr.server.repository.UserRepository;
import ai.aipr.server.service.RateLimiterService;
import ai.aipr.server.service.RateLimiterService.RateLimitResult;
import ai.aipr.server.service.RateLimiterService.Tier;
import ai.aipr.server.session.SessionManager;
import ai.aipr.server.session.SessionManager.ValidatedSession;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.autoconfigure.condition.ConditionalOnBean;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Component;
import org.springframework.web.servlet.HandlerInterceptor;

/**
 * HTTP interceptor that enforces rate limits on API requests.
 *
 * Extracts user identity from session or falls back to IP-based limiting.
 * Returns 429 Too Many Requests when rate limit is exceeded.
 */
@Component
@ConditionalOnBean(RedisConfig.class)
public class RateLimitInterceptor implements HandlerInterceptor {

    private static final Logger log = LoggerFactory.getLogger(RateLimitInterceptor.class);

    private static final String AUTHORIZATION_HEADER = "Authorization";
    private static final String BEARER_PREFIX = "Bearer ";
    private static final String X_FORWARDED_FOR = "X-Forwarded-For";
    private static final String X_REAL_IP = "X-Real-IP";

    // Response headers for rate limiting
    private static final String X_RATELIMIT_LIMIT = "X-RateLimit-Limit";
    private static final String X_RATELIMIT_REMAINING = "X-RateLimit-Remaining";
    private static final String X_RATELIMIT_RESET = "X-RateLimit-Reset";
    private static final String RETRY_AFTER = "Retry-After";

    private final RateLimiterService rateLimiterService;
    private final SessionManager sessionManager;
    private final UserRepository userRepository;

    public RateLimitInterceptor(RateLimiterService rateLimiterService,
                                SessionManager sessionManager,
                                UserRepository userRepository) {
        this.rateLimiterService = rateLimiterService;
        this.sessionManager = sessionManager;
        this.userRepository = userRepository;
    }

    @Override
    public boolean preHandle(@NotNull HttpServletRequest request, @NotNull HttpServletResponse response,
                             @NotNull Object handler) throws Exception {

        // Skip rate limiting for health checks and static resources
        String path = request.getRequestURI();
        if (isExemptPath(path)) {
            return true;
        }

        // Extract user identity
        String userId = extractUserId(request);
        Tier tier = determineTier(request, userId);

        // Check rate limit
        RateLimitResult result = rateLimiterService.checkRateLimit(userId, tier);

        // Set rate limit headers
        response.setHeader(X_RATELIMIT_LIMIT, String.valueOf(tier.getCapacity()));
        response.setHeader(X_RATELIMIT_REMAINING, String.valueOf(result.remainingTokens()));

        if (!result.allowed()) {
            log.warn("Rate limit exceeded for {} on {}", userId, path);

            response.setHeader(RETRY_AFTER, String.valueOf(result.retryAfterMs() / 1000));
            response.setHeader(X_RATELIMIT_RESET,
                String.valueOf(System.currentTimeMillis() / 1000 + result.retryAfterMs() / 1000));

            response.setStatus(HttpStatus.TOO_MANY_REQUESTS.value());
            response.setContentType("application/json");
            response.getWriter().write("""
                {
                    "error": "rate_limit_exceeded",
                    "message": "Too many requests. Please retry after %d seconds.",
                    "retryAfterSeconds": %d
                }
                """.formatted(result.retryAfterMs() / 1000, result.retryAfterMs() / 1000));

            return false;
        }

        return true;
    }

    /**
     * Extract user ID from request (session or IP-based).
     */
    private String extractUserId(@NotNull HttpServletRequest request) {
        // Try to extract from Authorization header
        String authHeader = request.getHeader(AUTHORIZATION_HEADER);
        if (authHeader != null && authHeader.startsWith(BEARER_PREFIX)) {
            String token = authHeader.substring(BEARER_PREFIX.length());
            ValidatedSession session = sessionManager.validateSession(token);
            if (session != null) {
                return session.userId();
            }
        }

        // Fall back to IP-based identification
        return "ip:" + getClientIp(request);
    }

    /**
     * Determine rate limit tier based on user's subscription in database.
     */
    private Tier determineTier(HttpServletRequest request, @NotNull String userId) {
        // IP-based users get FREE tier
        if (userId.startsWith("ip:")) {
            return Tier.FREE;
        }

        try {
            String tierName = userRepository.findTierByUserId(userId);
            return switch (tierName.toUpperCase()) {
                case "PRO" -> Tier.PRO;
                case "ENTERPRISE" -> Tier.ENTERPRISE;
                default -> Tier.FREE;
            };
        } catch (Exception e) {
            log.debug("Failed to look up tier for user {}, defaulting to FREE: {}", userId, e.getMessage());
            return Tier.FREE;
        }
    }

    /**
     * Get client IP address, respecting proxy headers.
     */
    private String getClientIp(@NotNull HttpServletRequest request) {
        String xForwardedFor = request.getHeader(X_FORWARDED_FOR);
        if (xForwardedFor != null && !xForwardedFor.isEmpty()) {
            // Take the first IP (original client)
            return xForwardedFor.split(",")[0].trim();
        }

        String xRealIp = request.getHeader(X_REAL_IP);
        if (xRealIp != null && !xRealIp.isEmpty()) {
            return xRealIp;
        }

        return request.getRemoteAddr();
    }

    /**
     * Check if path is exempt from rate limiting.
     */
    private boolean isExemptPath(@NotNull String path) {
        return path.startsWith("/actuator/") ||
               path.equals("/health") ||
               path.equals("/ready") ||
               path.equals("/live") ||
               path.startsWith("/static/") ||
               path.startsWith("/favicon");
    }
}
