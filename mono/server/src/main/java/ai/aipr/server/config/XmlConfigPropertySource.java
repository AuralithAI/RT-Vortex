package ai.aipr.server.config;

import org.jetbrains.annotations.NotNull;
import org.springframework.core.env.EnumerablePropertySource;

import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Spring {@link org.springframework.core.env.PropertySource} backed by
 * {@code rtserverprops.xml} and {@code vcsplatforms.xml} via {@link Environment}.
 *
 * <p>Maps XML flattened keys to the Spring property keys that {@code @Value}
 * annotations and Spring Boot autoconfiguration expect. For example:</p>
 * <ul>
 *   <li>{@code database.url} → {@code spring.datasource.url}</li>
 *   <li>{@code engine.host} → {@code aipr.engine.host}</li>
 *   <li>{@code github.api-url} → {@code aipr.auth.github.api-url}</li>
 * </ul>
 *
 * <p>Registered by {@link ai.aipr.server.config.XmlConfigEnvironmentPostProcessor} before any
 * {@code @Value} resolution occurs.</p>
 */
public class XmlConfigPropertySource extends EnumerablePropertySource<Map<String, String>> {

    public static final String SOURCE_NAME = "aipr-xml-config";

    private final Map<String, String> properties;

    public XmlConfigPropertySource() {
        super(SOURCE_NAME, new LinkedHashMap<>());
        this.properties = buildPropertyMap();
    }

    @Override
    public Object getProperty(@NotNull String name) {
        return properties.get(name);
    }

    @Override
    @NotNull
    public String[] getPropertyNames() {
        return properties.keySet().toArray(String[]::new);
    }

    // =========================================================================
    // Build the mapping from XML keys → Spring property keys
    // =========================================================================

    @NotNull
    private Map<String, String> buildPropertyMap() {
        Map<String, String> props = new LinkedHashMap<>();

        // Load XML config
        Environment.ConfigReader server = Environment.server();
        Environment.ConfigReader platforms = Environment.platforms();

        // -----------------------------------------------------------------
        // REST Server
        // -----------------------------------------------------------------
        map(props, "server.port",                  server.get("server.port", "8080"));
        map(props, "server.shutdown",              server.get("server.shutdown", "graceful"));

        // -----------------------------------------------------------------
        // Spring application
        // -----------------------------------------------------------------
        map(props, "spring.application.name",      "aipr-server");

        // -----------------------------------------------------------------
        // Database → Spring DataSource + Hikari
        // -----------------------------------------------------------------
        map(props, "spring.datasource.url",                              server.get("database.url"));
        map(props, "spring.datasource.username",                         server.get("database.username"));
        map(props, "spring.datasource.password",                         server.get("database.password"));
        map(props, "spring.datasource.driver-class-name",                server.get("database.driver", "org.postgresql.Driver"));
        map(props, "spring.datasource.hikari.pool-name",                 server.get("database.pool.name", "aipr-pool"));
        map(props, "spring.datasource.hikari.maximum-pool-size",         server.get("database.pool.max-size", "20"));
        map(props, "spring.datasource.hikari.minimum-idle",              server.get("database.pool.min-idle", "5"));
        map(props, "spring.datasource.hikari.connection-timeout",        server.get("database.pool.connection-timeout-ms", "30000"));
        map(props, "spring.datasource.hikari.idle-timeout",              server.get("database.pool.idle-timeout-ms", "600000"));
        map(props, "spring.datasource.hikari.max-lifetime",              server.get("database.pool.max-lifetime-ms", "1800000"));
        map(props, "spring.datasource.hikari.leak-detection-threshold",  server.get("database.pool.leak-detection-threshold-ms", "60000"));


        // Flyway
        map(props, "spring.flyway.enabled",                server.get("database.flyway.enabled", "true"));
        map(props, "spring.flyway.locations",              server.get("database.flyway.locations", "classpath:db/migration"));
        map(props, "spring.flyway.baseline-on-migrate",    server.get("database.flyway.baseline-on-migrate", "true"));

        // -----------------------------------------------------------------
        // Redis
        // -----------------------------------------------------------------
        map(props, "spring.data.redis.host",                      server.get("redis.host"));
        map(props, "spring.data.redis.port",                      server.get("redis.port", "6379"));
        map(props, "spring.data.redis.password",                  server.get("redis.password", ""));
        map(props, "spring.data.redis.database",                  server.get("redis.database", "0"));
        map(props, "spring.data.redis.timeout",                   server.get("redis.timeout-ms", "5000") + "ms");
        map(props, "spring.data.redis.lettuce.pool.max-active",   server.get("redis.pool.max-active", "16"));
        map(props, "spring.data.redis.lettuce.pool.max-idle",     server.get("redis.pool.max-idle", "8"));
        map(props, "spring.data.redis.lettuce.pool.min-idle",     server.get("redis.pool.min-idle", "2"));
        map(props, "spring.data.redis.lettuce.pool.max-wait",     server.get("redis.pool.max-wait-ms", "5000") + "ms");

        // Cache
        map(props, "spring.cache.type",            "caffeine");
        map(props, "spring.cache.caffeine.spec",   "maximumSize=1000,expireAfterWrite=300s");

        // Jackson
        map(props, "spring.jackson.serialization.write-dates-as-timestamps",    "false");
        map(props, "spring.jackson.deserialization.fail-on-unknown-properties",  "false");
        map(props, "spring.jackson.default-property-inclusion",                  "non_null");

        // -----------------------------------------------------------------
        // gRPC Server
        // -----------------------------------------------------------------
        map(props, "grpc.server.port",                                 server.get("grpc-server.port", "9090"));
        map(props, "grpc.server.security.enabled",                     server.get("grpc-server.security.enabled", "true"));
        map(props, "grpc.server.security.certificateChain",            prefixFile(server.get("grpc-server.security.cert-chain", "")));
        map(props, "grpc.server.security.privateKey",                  prefixFile(server.get("grpc-server.security.private-key", "")));
        map(props, "grpc.server.security.trustCertCollection",         prefixFile(server.get("grpc-server.security.trust-certs", "")));
        map(props, "grpc.server.security.clientAuth",                  server.get("grpc-server.security.client-auth", "OPTIONAL"));

        // -----------------------------------------------------------------
        // Actuator
        // -----------------------------------------------------------------
        map(props, "management.endpoints.web.exposure.include",    "health,info,prometheus,metrics");
        map(props, "management.endpoint.health.show-details",      "when_authorized");
        map(props, "management.metrics.tags.application",          "aipr-server");
        map(props, "management.metrics.export.prometheus.enabled", "true");

        // -----------------------------------------------------------------
        // aipr.engine.* — C++ Engine gRPC Client
        // -----------------------------------------------------------------
        map(props, "aipr.engine.host",                     server.get("engine.host", "localhost"));
        map(props, "aipr.engine.port",                     server.get("engine.port", "50051"));
        map(props, "aipr.engine.timeout-ms",               server.get("engine.timeout-ms", "30000"));
        map(props, "aipr.engine.negotiation-type",         server.get("engine.negotiation-type", "TLS"));
        map(props, "aipr.engine.tls.cert-chain",           server.get("engine.tls.cert-chain", ""));
        map(props, "aipr.engine.tls.private-key",          server.get("engine.tls.private-key", ""));
        map(props, "aipr.engine.tls.trust-certs",          server.get("engine.tls.trust-certs", ""));

        // -----------------------------------------------------------------
        // aipr.llm.* — LLM Provider
        // -----------------------------------------------------------------
        map(props, "aipr.llm.provider",      server.get("llm.provider", "openai-compatible"));
        map(props, "aipr.llm.base-url",      server.get("llm.base-url", "https://api.openai.com/v1"));
        map(props, "aipr.llm.api-key",       server.get("llm.api-key", ""));
        map(props, "aipr.llm.model",         server.get("llm.model", "gpt-4-turbo-preview"));
        map(props, "aipr.llm.max-tokens",    server.get("llm.max-tokens", "4096"));
        map(props, "aipr.llm.temperature",   server.get("llm.temperature", "0.1"));
        map(props, "aipr.llm.timeout-ms",    server.get("llm.timeout-ms", "120000"));

        // -----------------------------------------------------------------
        // aipr.review.*
        // -----------------------------------------------------------------
        map(props, "aipr.review.max-diff-size",             server.get("review.max-diff-size", "500000"));
        map(props, "aipr.review.max-files-per-pr",          server.get("review.max-files-per-pr", "100"));
        map(props, "aipr.review.max-comments-per-review",   server.get("review.max-comments", "50"));
        map(props, "aipr.review.enable-heuristics",         server.get("review.enable-heuristics", "true"));
        map(props, "aipr.review.enable-context-retrieval",  server.get("review.enable-context-retrieval", "true"));

        // -----------------------------------------------------------------
        // aipr.security.*
        // -----------------------------------------------------------------
        map(props, "aipr.security.jwt-secret",         server.get("security.jwt-secret", ""));
        map(props, "aipr.security.jwt-expiration-ms",  server.get("security.jwt-expiration-ms", "3600000"));
        map(props, "aipr.security.allowed-origins",    server.get("security.allowed-origins", "http://localhost:3000"));
        map(props, "aipr.security.encryption-key",     server.get("security.encryption-key", ""));

        // -----------------------------------------------------------------
        // aipr.storage.*
        // -----------------------------------------------------------------
        map(props, "aipr.storage.type",             server.get("storage.type", "local"));
        map(props, "aipr.storage.local.base-path",  server.get("storage.local.base-path", "./data"));
        map(props, "aipr.storage.s3.bucket",        server.get("storage.s3.bucket", ""));
        map(props, "aipr.storage.s3.region",        server.get("storage.s3.region", "us-east-1"));

        // -----------------------------------------------------------------
        // aipr.repositories.*
        // -----------------------------------------------------------------
        map(props, "aipr.repositories.base-path",   server.get("repositories.base-path", "./repos"));

        // -----------------------------------------------------------------
        // aipr.auth.github.* — from vcsplatforms.xml
        // -----------------------------------------------------------------
        map(props, "aipr.auth.github.enabled",          platforms.get("github.enabled", "true"));
        map(props, "aipr.auth.github.base-url",         platforms.get("github.base-url", "https://github.com"));
        map(props, "aipr.auth.github.api-url",          platforms.get("github.api-url", "https://api.github.com"));
        map(props, "aipr.auth.github.client-id",        platforms.get("github.oauth.client-id", ""));
        map(props, "aipr.auth.github.client-secret",    platforms.get("github.oauth.client-secret", ""));
        map(props, "aipr.auth.github.app-id",           platforms.get("github.app.app-id", ""));
        map(props, "aipr.auth.github.private-key-path", platforms.get("github.app.private-key-path", ""));
        map(props, "aipr.auth.github.webhook-secret",   platforms.get("github.webhook.secret", ""));
        map(props, "aipr.auth.github.token",            platforms.get("github.token.value", ""));

        // -----------------------------------------------------------------
        // aipr.auth.gitlab.* — from vcsplatforms.xml
        // -----------------------------------------------------------------
        map(props, "aipr.auth.gitlab.enabled",              platforms.get("gitlab.enabled", "false"));
        map(props, "aipr.auth.gitlab.base-url",             platforms.get("gitlab.base-url", "https://gitlab.com"));
        map(props, "aipr.auth.gitlab.application-id",       platforms.get("gitlab.oauth.application-id", ""));
        map(props, "aipr.auth.gitlab.application-secret",   platforms.get("gitlab.oauth.application-secret", ""));
        map(props, "aipr.auth.gitlab.webhook-secret",       platforms.get("gitlab.webhook.secret", ""));
        map(props, "aipr.auth.gitlab.token",                platforms.get("gitlab.token.value", ""));

        // -----------------------------------------------------------------
        // aipr.auth.bitbucket.* — from vcsplatforms.xml
        // -----------------------------------------------------------------
        map(props, "aipr.auth.bitbucket.enabled",        platforms.get("bitbucket.enabled", "false"));
        map(props, "aipr.auth.bitbucket.base-url",       platforms.get("bitbucket.base-url", "https://bitbucket.org"));
        map(props, "aipr.auth.bitbucket.api-url",        platforms.get("bitbucket.api-url", "https://api.bitbucket.org/2.0"));
        map(props, "aipr.auth.bitbucket.client-id",      platforms.get("bitbucket.oauth.client-id", ""));
        map(props, "aipr.auth.bitbucket.client-secret",  platforms.get("bitbucket.oauth.client-secret", ""));
        map(props, "aipr.auth.bitbucket.webhook-secret", platforms.get("bitbucket.webhook.secret", ""));
        map(props, "aipr.auth.bitbucket.username",       platforms.get("bitbucket.credentials.username", ""));
        map(props, "aipr.auth.bitbucket.app-password",   platforms.get("bitbucket.credentials.app-password", ""));
        map(props, "aipr.auth.bitbucket.token",          platforms.get("bitbucket.credentials.token", ""));

        // -----------------------------------------------------------------
        // Logging
        // -----------------------------------------------------------------
        map(props, "logging.level.root",     server.get("logging.root-level", "INFO"));
        map(props, "logging.level.ai.aipr",  server.get("logging.app-level", "DEBUG"));

        return props;
    }

    // =========================================================================
    // Helpers
    // =========================================================================

    private static void map(Map<String, String> props, String springKey, String value) {
        if (value != null) {
            props.put(springKey, value);
        }
    }

    /** Prefix cert paths with "file:" for Spring gRPC TLS config. */
    @NotNull
    private static String prefixFile(String path) {
        if (path == null || path.isBlank()) return "";
        if (path.startsWith("file:") || path.startsWith("classpath:")) return path;
        return "file:" + path;
    }
}


