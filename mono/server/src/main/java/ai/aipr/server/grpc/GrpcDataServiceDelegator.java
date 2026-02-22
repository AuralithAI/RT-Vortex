package ai.aipr.server.grpc;

import ai.aipr.server.config.Environment;
import io.grpc.ConnectivityState;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.health.v1.HealthCheckRequest;
import io.grpc.health.v1.HealthCheckResponse;
import io.grpc.health.v1.HealthGrpc;
import io.grpc.netty.shaded.io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.shaded.io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.shaded.io.netty.handler.ssl.SslContext;
import io.grpc.netty.shaded.io.netty.handler.ssl.SslContextBuilder;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.File;
import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Manages gRPC channel connections and provides load balancing/failover.
 *
 * <p>This delegator is responsible for:</p>
 * <ul>
 *   <li>Creating and managing gRPC channels</li>
 *   <li>Health checking connections</li>
 *   <li>Load balancing across multiple server instances</li>
 *   <li>Automatic reconnection on failure</li>
 * </ul>
 */
public class GrpcDataServiceDelegator {

    private static final Logger log = LoggerFactory.getLogger(GrpcDataServiceDelegator.class);

    private final GrpcConnectionConfig config;
    private final List<ServerInstance> serverInstances;
    private final AtomicReference<ManagedChannel> primaryChannel;
    private final AtomicInteger roundRobinIndex;
    private volatile boolean shutdown = false;

    /**
     * Server instance configuration.
     */
    public static class ServerInstance {
        private final String host;
        private final int port;
        private final int weight;

        public ServerInstance(String host, int port, int weight) {
            this.host = host;
            this.port = port;
            this.weight = weight;
        }

        public String getHost() { return host; }
        public int getPort() { return port; }
        public int getWeight() { return weight; }
        public String getAddress() { return host + ":" + port; }
    }

    /**
     * Create a new GrpcDataServiceDelegator.
     *
     * @param config          Connection configuration
     * @param serverInstances List of server instances to connect to
     */
    public GrpcDataServiceDelegator(GrpcConnectionConfig config,
                                    @NotNull List<ServerInstance> serverInstances) {
        this.config = config;
        this.serverInstances = serverInstances;
        this.primaryChannel = new AtomicReference<>();
        this.roundRobinIndex = new AtomicInteger(0);

        // Initialize primary channel
        if (!serverInstances.isEmpty()) {
            this.primaryChannel.set(createChannel(serverInstances.getFirst()));
        }
    }

    /**
     * Create a delegator for a single server.
     */
    @NotNull public static GrpcDataServiceDelegator forSingleServer(String host, int port) {
        return new GrpcDataServiceDelegator(
            GrpcConnectionConfig.defaults(),
            List.of(new ServerInstance(host, port, 1))
        );
    }

    /**
     * Create a delegator from XML configuration (rtserverprops.xml).
     * Reads engine Host and engine Port from the server configuration.
     *
     * @return GrpcDataServiceDelegator configured from XML
     */
    @NotNull
    public static GrpcDataServiceDelegator fromConfig() {
        try {
            Environment.ConfigReader config = Environment.server();
            String host = config.get("engine.host", "localhost");
            int port = config.getInt("engine.port", 50051);
            log.info("Creating gRPC delegator from config: {}:{}", host, port);
            return forSingleServer(host, port);
        } catch (Exception e) {
            log.warn("Failed to read gRPC config from XML, using defaults: {}", e.getMessage());
            return forSingleServer("localhost", 50051);
        }
    }

    /**
     * Get the primary gRPC channel.
     */
    public ManagedChannel getChannel() {
        if (shutdown) {
            throw new IllegalStateException("GrpcDataServiceDelegator has been shut down");
        }
        ManagedChannel channel = primaryChannel.get();
        if (channel == null || isChannelDead(channel)) {
            synchronized (this) {
                channel = primaryChannel.get();
                if (channel == null || isChannelDead(channel)) {
                    channel = selectHealthyChannel();
                    primaryChannel.set(channel);
                }
            }
        }
        return channel;
    }

    /**
     * Get a channel using weighted round-robin load balancing.
     * Instances with higher weight receive proportionally more requests.
     */
    public ManagedChannel getChannelRoundRobin() {
        if (shutdown) {
            throw new IllegalStateException("GrpcDataServiceDelegator has been shut down");
        }
        if (serverInstances.size() <= 1) {
            return getChannel();
        }

        // Build weighted list: instance with weight=3 appears 3 times
        int totalWeight = serverInstances.stream().mapToInt(ServerInstance::getWeight).sum();
        if (totalWeight <= 0) totalWeight = serverInstances.size();

        int index = Math.abs(roundRobinIndex.getAndIncrement()) % totalWeight;
        int cumulative = 0;
        ServerInstance selected = serverInstances.getFirst();
        for (ServerInstance instance : serverInstances) {
            cumulative += Math.max(instance.getWeight(), 1);
            if (index < cumulative) {
                selected = instance;
                break;
            }
        }

        log.debug("Round-robin selected: {} (weight={})", selected.getAddress(), selected.getWeight());
        return createChannel(selected);
    }

    /**
     * Check if the connection is healthy.
     */
    public boolean isHealthy() {
        if (shutdown) {
            return false;
        }
        ManagedChannel channel = primaryChannel.get();
        if (channel == null) {
            return false;
        }

        ConnectivityState state = channel.getState(false);
        return state == ConnectivityState.READY || state == ConnectivityState.IDLE;
    }

    /**
     * Perform a health check against the server.
     */
    public boolean healthCheck() {
        try {
            ManagedChannel channel = getChannel();
            HealthGrpc.HealthBlockingStub healthStub = HealthGrpc.newBlockingStub(channel);

            HealthCheckResponse response = healthStub
                .withDeadlineAfter(config.getHealthCheckTimeoutMs(), TimeUnit.MILLISECONDS)
                .check(HealthCheckRequest.newBuilder().build());

            return response.getStatus() == HealthCheckResponse.ServingStatus.SERVING;
        } catch (Exception e) {
            log.warn("Health check failed: {}", e.getMessage());
            return false;
        }
    }

    /**
     * Force reconnection.
     */
    public void reconnect() {
        synchronized (this) {
            ManagedChannel oldChannel = primaryChannel.get();
            if (oldChannel != null) {
                oldChannel.shutdown();
            }

            ManagedChannel newChannel = selectHealthyChannel();
            primaryChannel.set(newChannel);

            log.info("Reconnected to gRPC server");
        }
    }

    /**
     * Get the primary server address.
     *
     * @return Server address in "host:port" format
     */
    public String getServerAddress() {
        if (!serverInstances.isEmpty()) {
            return serverInstances.getFirst().getAddress();
        }
        return "localhost:50051";
    }

    /**
     * Check if multiple engine instances are configured.
     */
    public boolean hasMultipleInstances() {
        return serverInstances.size() > 1;
    }

    /**
     * Shutdown all channels.
     */
    public void shutdown(long timeout, TimeUnit unit) throws InterruptedException {
        shutdown = true;
        ManagedChannel channel = primaryChannel.get();
        if (channel != null) {
            channel.shutdown();
            if (!channel.awaitTermination(timeout, unit)) {
                channel.shutdownNow();
            }
        }
    }

    // =========================================================================
    // Internal
    // =========================================================================

    private ManagedChannel createChannel(@NotNull ServerInstance instance) {
        log.debug("Creating gRPC channel to {}", instance.getAddress());

        if (config.isUsePlaintext()) {
            ManagedChannelBuilder<?> builder = ManagedChannelBuilder
                .forAddress(instance.getHost(), instance.getPort())
                .usePlaintext();

            applyCommonConfig(builder);
            return builder.build();
        }

        // TLS / mTLS
        try {
            NettyChannelBuilder builder = NettyChannelBuilder
                .forAddress(instance.getHost(), instance.getPort());

            SslContextBuilder sslBuilder = GrpcSslContexts.forClient();

            if (config.getTrustCertsPath() != null) {
                File trustCerts = new File(config.getTrustCertsPath());
                if (trustCerts.exists()) {
                    sslBuilder.trustManager(trustCerts);
                    log.debug("TLS trust certs: {}", trustCerts.getAbsolutePath());
                } else {
                    log.warn("Trust cert file not found: {}", config.getTrustCertsPath());
                }
            }

            // Client certificate + key (for mTLS)
            if (config.getCertChainPath() != null && config.getPrivateKeyPath() != null) {
                File certChain = new File(config.getCertChainPath());
                File privateKey = new File(config.getPrivateKeyPath());
                if (certChain.exists() && privateKey.exists()) {
                    sslBuilder.keyManager(certChain, privateKey);
                    log.debug("mTLS client cert: {}, key: {}",
                            certChain.getAbsolutePath(), privateKey.getAbsolutePath());
                } else {
                    log.warn("Client cert/key files not found: cert={}, key={}",
                            config.getCertChainPath(), config.getPrivateKeyPath());
                }
            }

            SslContext sslContext = sslBuilder.build();
            builder.sslContext(sslContext);

            applyCommonConfig(builder);
            log.info("Created TLS gRPC channel to {}", instance.getAddress());
            return builder.build();
        } catch (Exception e) {
            log.error("Failed to create TLS channel to {}, falling back to plaintext: {}",
                    instance.getAddress(), e.getMessage(), e);
            // Fallback to plaintext on TLS failure (dev safety net)
            ManagedChannelBuilder<?> fallback = ManagedChannelBuilder
                .forAddress(instance.getHost(), instance.getPort())
                .usePlaintext();
            applyCommonConfig(fallback);
            return fallback.build();
        }
    }

    private void applyCommonConfig(@NotNull ManagedChannelBuilder<?> builder) {
        builder.keepAliveTime(config.getKeepAliveTimeSeconds(), TimeUnit.SECONDS)
               .keepAliveTimeout(config.getKeepAliveTimeoutSeconds(), TimeUnit.SECONDS)
               .keepAliveWithoutCalls(config.isKeepAliveWithoutCalls())
               .maxInboundMessageSize(config.getMaxInboundMessageSize())
               .idleTimeout(config.getIdleTimeoutSeconds(), TimeUnit.SECONDS);

        if (config.isRetryEnabled()) {
            builder.enableRetry()
                   .maxRetryAttempts(config.getMaxRetryAttempts());
        }
    }

    private ManagedChannel selectHealthyChannel() {
        for (ServerInstance instance : serverInstances) {
            try {
                ManagedChannel channel = createChannel(instance);
                // Quick connectivity check
                ConnectivityState state = channel.getState(true);
                if (state != ConnectivityState.TRANSIENT_FAILURE) {
                    log.info("Selected server instance: {}", instance.getAddress());
                    return channel;
                }
                channel.shutdown();
            } catch (Exception e) {
                log.warn("Failed to connect to {}: {}", instance.getAddress(), e.getMessage());
            }
        }

        // Fallback to first instance
        if (!serverInstances.isEmpty()) {
            log.warn("No healthy instances found, falling back to first instance");
            return createChannel(serverInstances.getFirst());
        }

        throw new IllegalStateException("No server instances configured");
    }

    private boolean isChannelDead(@NotNull ManagedChannel channel) {
        if (channel.isShutdown() || channel.isTerminated()) {
            return true;
        }
        ConnectivityState state = channel.getState(false);
        return state == ConnectivityState.SHUTDOWN;
    }
}
