package ai.aipr.server.service;

import ai.aipr.server.config.RedisConfig;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.autoconfigure.condition.ConditionalOnBean;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.data.redis.core.script.DefaultRedisScript;
import org.springframework.stereotype.Service;

import java.time.Instant;
import java.util.Collections;
import java.util.List;

/**
 * Token-bucket rate limiter using Redis for distributed rate limiting.
 * Implements a sliding window rate limiter that allows bursting while
 * enforcing a sustained request rate over time.
 */
@Service
@ConditionalOnBean(RedisConfig.class)
public class RateLimiterService {

    private static final Logger log = LoggerFactory.getLogger(RateLimiterService.class);

    private final RedisTemplate<String, Object> redisTemplate;

    // Lua script for atomic token bucket operation
    private static final String TOKEN_BUCKET_SCRIPT = """
        local key = KEYS[1]
        local capacity = tonumber(ARGV[1])
        local refillRate = tonumber(ARGV[2])
        local now = tonumber(ARGV[3])
        local requested = tonumber(ARGV[4])

        local bucket = redis.call('HMGET', key, 'tokens', 'lastRefill')
        local tokens = tonumber(bucket[1])
        local lastRefill = tonumber(bucket[2])

        if tokens == nil then
            tokens = capacity
            lastRefill = now
        else
            local elapsed = now - lastRefill
            local refill = math.floor(elapsed * refillRate / 1000)
            tokens = math.min(capacity, tokens + refill)
            lastRefill = now
        end

        local allowed = 0
        if tokens >= requested then
            tokens = tokens - requested
            allowed = 1
        end

        redis.call('HMSET', key, 'tokens', tokens, 'lastRefill', lastRefill)
        redis.call('EXPIRE', key, 3600)

        return {allowed, tokens}
        """;

    private final DefaultRedisScript<List<Long>> tokenBucketScript;

    public RateLimiterService(RedisTemplate<String, Object> redisTemplate) {
        this.redisTemplate = redisTemplate;
        this.tokenBucketScript = new DefaultRedisScript<>();
        this.tokenBucketScript.setScriptText(TOKEN_BUCKET_SCRIPT);
        @SuppressWarnings("unchecked")
        Class<List<Long>> resultType = (Class<List<Long>>) (Class<?>) List.class;
        this.tokenBucketScript.setResultType(resultType);
    }

    /**
     * Rate limit configuration for different tiers.
     */
    public enum Tier {
        FREE(60, 1),           // 60 requests/minute, refill 1/second
        PRO(300, 5),           // 300 requests/minute, refill 5/second
        ENTERPRISE(1000, 20),  // 1000 requests/minute, refill 20/second
        UNLIMITED(Integer.MAX_VALUE, Integer.MAX_VALUE);

        private final int capacity;
        private final int refillRatePerSecond;

        Tier(int capacity, int refillRatePerSecond) {
            this.capacity = capacity;
            this.refillRatePerSecond = refillRatePerSecond;
        }

        public int getCapacity() { return capacity; }
        public int getRefillRatePerSecond() { return refillRatePerSecond; }
    }

    /**
     * Rate limit result.
     */
    public record RateLimitResult(
        boolean allowed,
        int remainingTokens,
        long retryAfterMs
    ) {
        @NotNull
        public static RateLimitResult allowed(int remaining) {
            return new RateLimitResult(true, remaining, 0);
        }

        @NotNull
        public static RateLimitResult denied(int remaining, long retryAfterMs) {
            return new RateLimitResult(false, remaining, retryAfterMs);
        }
    }

    /**
     * Check if a request is allowed underrate limits.
     *
     * @param userId The user ID or identifier
     * @param tier   The user's rate limit tier
     * @return Rate limit result indicating if request is allowed
     */
    public RateLimitResult checkRateLimit(String userId, Tier tier) {
        return checkRateLimit(userId, tier, 1);
    }

    /**
     * Check if a request is allowed underrate limits with custom token cost.
     *
     * @param userId    The user ID or identifier
     * @param tier      The user's rate limit tier
     * @param tokenCost Number of tokens to consume (default 1)
     * @return Rate limit result indicating if request is allowed
     */
    public RateLimitResult checkRateLimit(String userId, Tier tier, int tokenCost) {
        if (tier == Tier.UNLIMITED) {
            return RateLimitResult.allowed(Integer.MAX_VALUE);
        }

        String key = RedisConfig.Keys.rateLimit(userId);
        long now = Instant.now().toEpochMilli();

        try {
            List<Long> result = redisTemplate.execute(
                tokenBucketScript,
                Collections.singletonList(key),
                tier.getCapacity(),
                tier.getRefillRatePerSecond(),
                now,
                tokenCost
            );

            if (result.size() < 2) {
                log.warn("Rate limit script returned unexpected result for user {}", userId);
                return RateLimitResult.allowed(tier.getCapacity());  // Fail open
            }

            boolean allowed = result.get(0) == 1;
            int remainingTokens = result.get(1).intValue();

            if (allowed) {
                return RateLimitResult.allowed(remainingTokens);
            } else {
                // Calculate retry-after based on refill rate
                long retryAfterMs = (tokenCost - remainingTokens) * 1000L / tier.getRefillRatePerSecond();
                log.debug("Rate limit exceeded for user {}, retry after {}ms", userId, retryAfterMs);
                return RateLimitResult.denied(remainingTokens, Math.max(retryAfterMs, 1000));
            }

        } catch (Exception e) {
            log.error("Rate limit check failed for user {}: {}", userId, e.getMessage());
            // Fail open - allow request if Redis is unavailable
            return RateLimitResult.allowed(tier.getCapacity());
        }
    }

    /**
     * Reset rate limit for a user (for testing or admin purposes).
     */
    public void resetRateLimit(String userId) {
        String key = RedisConfig.Keys.rateLimit(userId);
        redisTemplate.delete(key);
        log.info("Reset rate limit for user {}", userId);
    }

    /**
     * Get remaining tokens for a user without consuming any.
     */
    public int getRemainingTokens(String userId, Tier tier) {
        String key = RedisConfig.Keys.rateLimit(userId);

        try {
            Object tokensObj = redisTemplate.opsForHash().get(key, "tokens");
            if (tokensObj == null) {
                return tier.getCapacity();
            }
            return ((Number) tokensObj).intValue();
        } catch (Exception e) {
            log.error("Failed to get remaining tokens for user {}: {}", userId, e.getMessage());
            return tier.getCapacity();
        }
    }
}
