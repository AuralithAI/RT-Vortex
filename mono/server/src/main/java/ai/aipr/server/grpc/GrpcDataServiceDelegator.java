package ai.aipr.server.grpc;

import ai.aipr.server.config.Environment;
import io.grpc.ConnectivityState;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.health.v1.HealthCheckRequest;
import io.grpc.health.v1.HealthCheckResponse;
import io.grpc.health.v1.HealthGrpc;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

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
                                     List<ServerInstance> serverInstances) {
        this.config = config;
        this.serverInstances = serverInstances;
        this.primaryChannel = new AtomicReference<>();
        this.roundRobinIndex = new AtomicInteger(0);
        
        // Initialize primary channel
        if (!serverInstances.isEmpty()) {
            this.primaryChannel.set(createChannel(serverInstances.get(0)));
        }
    }

    /**
     * Create a delegator for a single server.
     */
    public static GrpcDataServiceDelegator forSingleServer(String host, int port) {
        return new GrpcDataServiceDelegator(
            GrpcConnectionConfig.defaults(),
            List.of(new ServerInstance(host, port, 1))
        );
    }
    
    /**
     * Create a delegator from XML configuration (rtserverprops.xml).
     * Reads engine.host and engine.port from the server configuration.
     *
     * @return GrpcDataServiceDelegator configured from XML
     */
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
     * Get a channel using round-robin load balancing.
     */
    public ManagedChannel getChannelRoundRobin() {
        if (serverInstances.size() <= 1) {
            return getChannel();
        }
        
        int index = roundRobinIndex.getAndIncrement() % serverInstances.size();
        ServerInstance instance = serverInstances.get(index);
        
        // For simplicity, create channel on demand (in production, use a pool)
        return createChannel(instance);
    }

    /**
     * Check if the connection is healthy.
     */
    public boolean isHealthy() {
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
            return serverInstances.get(0).getAddress();
        }
        return "localhost:50051"; // default fallback matching rtserverprops.xml
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

    private ManagedChannel createChannel(ServerInstance instance) {
        log.debug("Creating gRPC channel to {}", instance.getAddress());
        
        ManagedChannelBuilder<?> builder = ManagedChannelBuilder
            .forAddress(instance.getHost(), instance.getPort());
        
        if (config.isUsePlaintext()) {
            builder.usePlaintext();
        }
        
        builder.keepAliveTime(config.getKeepAliveTimeSeconds(), TimeUnit.SECONDS)
               .keepAliveTimeout(config.getKeepAliveTimeoutSeconds(), TimeUnit.SECONDS)
               .keepAliveWithoutCalls(config.isKeepAliveWithoutCalls())
               .maxInboundMessageSize(config.getMaxInboundMessageSize())
               .idleTimeout(config.getIdleTimeoutSeconds(), TimeUnit.SECONDS);
        
        // Add retry policy
        if (config.isRetryEnabled()) {
            builder.enableRetry()
                   .maxRetryAttempts(config.getMaxRetryAttempts());
        }
        
        return builder.build();
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
            return createChannel(serverInstances.get(0));
        }
        
        throw new IllegalStateException("No server instances configured");
    }

    private boolean isChannelDead(ManagedChannel channel) {
        if (channel.isShutdown() || channel.isTerminated()) {
            return true;
        }
        ConnectivityState state = channel.getState(false);
        return state == ConnectivityState.SHUTDOWN;
    }
}
