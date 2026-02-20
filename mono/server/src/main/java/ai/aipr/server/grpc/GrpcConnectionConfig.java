package ai.aipr.server.grpc;

/**
 * Configuration for gRPC connections.
 */
public class GrpcConnectionConfig {

    private boolean usePlaintext = true;
    private long keepAliveTimeSeconds = 60;
    private long keepAliveTimeoutSeconds = 20;
    private boolean keepAliveWithoutCalls = true;
    private int maxInboundMessageSize = 16 * 1024 * 1024; // 16MB
    private long idleTimeoutSeconds = 300;
    private boolean retryEnabled = true;
    private int maxRetryAttempts = 3;
    private long healthCheckTimeoutMs = 5000;
    private long deadlineMs = 30000;

    public static GrpcConnectionConfig defaults() {
        return new GrpcConnectionConfig();
    }

    // Getters
    public boolean isUsePlaintext() { return usePlaintext; }
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
