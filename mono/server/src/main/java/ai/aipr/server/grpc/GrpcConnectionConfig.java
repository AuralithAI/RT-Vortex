package ai.aipr.server.grpc;

import org.jetbrains.annotations.NotNull;

/**
 * Configuration for gRPC connections.
 */
public class GrpcConnectionConfig {

    private boolean usePlaintext = true;
    private String certChainPath;   // Client cert for mTLS
    private String privateKeyPath;  // Client key for mTLS
    private String trustCertsPath;  // CA cert to verify server
    private long keepAliveTimeSeconds = 60;
    private long keepAliveTimeoutSeconds = 20;
    private boolean keepAliveWithoutCalls = true;
    private int maxInboundMessageSize = 16 * 1024 * 1024; // 16MB
    private long idleTimeoutSeconds = 300;
    private boolean retryEnabled = true;
    private int maxRetryAttempts = 3;
    private long healthCheckTimeoutMs = 5000;
    private long deadlineMs = 30000;

    @NotNull
    public static GrpcConnectionConfig defaults() {
        return new GrpcConnectionConfig();
    }

    @NotNull
    public static GrpcConnectionConfig withTls(String certChainPath, String privateKeyPath, String trustCertsPath) {
        GrpcConnectionConfig config = new GrpcConnectionConfig();
        config.usePlaintext = false;
        config.certChainPath = certChainPath;
        config.privateKeyPath = privateKeyPath;
        config.trustCertsPath = trustCertsPath;
        return config;
    }

    // Getters
    public boolean isUsePlaintext() { return usePlaintext; }
    public String getCertChainPath() { return certChainPath; }
    public String getPrivateKeyPath() { return privateKeyPath; }
    public String getTrustCertsPath() { return trustCertsPath; }
    public long getKeepAliveTimeSeconds() { return keepAliveTimeSeconds; }
    public long getKeepAliveTimeoutSeconds() { return keepAliveTimeoutSeconds; }
    public boolean isKeepAliveWithoutCalls() { return keepAliveWithoutCalls; }
    public int getMaxInboundMessageSize() { return maxInboundMessageSize; }
    public long getIdleTimeoutSeconds() { return idleTimeoutSeconds; }
    public boolean isRetryEnabled() { return retryEnabled; }
    public int getMaxRetryAttempts() { return maxRetryAttempts; }
    public long getHealthCheckTimeoutMs() { return healthCheckTimeoutMs; }
    public long getDeadlineMs() { return deadlineMs; }

    // Builder-style setters
    public GrpcConnectionConfig usePlaintext(boolean usePlaintext) {
        this.usePlaintext = usePlaintext;
        return this;
    }

    public GrpcConnectionConfig certChainPath(String path) {
        this.certChainPath = path;
        return this;
    }

    public GrpcConnectionConfig privateKeyPath(String path) {
        this.privateKeyPath = path;
        return this;
    }

    public GrpcConnectionConfig trustCertsPath(String path) {
        this.trustCertsPath = path;
        return this;
    }

    public GrpcConnectionConfig keepAliveTimeSeconds(long seconds) {
        this.keepAliveTimeSeconds = seconds;
        return this;
    }

    public GrpcConnectionConfig keepAliveTimeoutSeconds(long seconds) {
        this.keepAliveTimeoutSeconds = seconds;
        return this;
    }

    public GrpcConnectionConfig keepAliveWithoutCalls(boolean enabled) {
        this.keepAliveWithoutCalls = enabled;
        return this;
    }

    public GrpcConnectionConfig maxInboundMessageSize(int size) {
        this.maxInboundMessageSize = size;
        return this;
    }

    public GrpcConnectionConfig idleTimeoutSeconds(long seconds) {
        this.idleTimeoutSeconds = seconds;
        return this;
    }

    public GrpcConnectionConfig retryEnabled(boolean enabled) {
        this.retryEnabled = enabled;
        return this;
    }

    public GrpcConnectionConfig maxRetryAttempts(int attempts) {
        this.maxRetryAttempts = attempts;
        return this;
    }

    public GrpcConnectionConfig healthCheckTimeoutMs(long timeout) {
        this.healthCheckTimeoutMs = timeout;
        return this;
    }

    public GrpcConnectionConfig deadlineMs(long deadline) {
        this.deadlineMs = deadline;
        return this;
    }
}
