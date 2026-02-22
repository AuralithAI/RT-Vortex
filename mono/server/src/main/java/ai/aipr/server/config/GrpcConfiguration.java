package ai.aipr.server.config;

import ai.aipr.server.grpc.GrpcConnectionConfig;
import ai.aipr.server.grpc.GrpcDataServiceDelegator;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

import java.util.List;

/**
 * Configuration for gRPC connections.
 *
 * <p>Creates and manages the {@link GrpcDataServiceDelegator} bean which provides
 * the managed gRPC channel for communicating with the C++ engine.</p>
 *
 * <p>TLS is determined by {@code aipr.engine.negotiation-type}:</p>
 * <ul>
 *   <li>{@code TLS} — Uses client certs from {@code aipr.engine.tls.*} for mTLS to the engine</li>
 *   <li>{@code PLAINTEXT} — No encryption (development only)</li>
 * </ul>
 */
@Configuration
public class GrpcConfiguration {

    private static final Logger log = LoggerFactory.getLogger(GrpcConfiguration.class);

    @Bean
    public GrpcConnectionConfig grpcConnectionConfig(
            @Value("${aipr.engine.negotiation-type:TLS}") String negotiationType,
            @Value("${aipr.engine.tls.cert-chain:}") String certChainPath,
            @Value("${aipr.engine.tls.private-key:}") String privateKeyPath,
            @Value("${aipr.engine.tls.trust-certs:}") String trustCertsPath) {

        boolean usePlaintext = "PLAINTEXT".equalsIgnoreCase(negotiationType);

        if (usePlaintext) {
            log.info("Engine gRPC connection: PLAINTEXT (no TLS)");
            return GrpcConnectionConfig.defaults();
        }

        log.info("Engine gRPC connection: TLS (cert={}, ca={})", certChainPath, trustCertsPath);
        return GrpcConnectionConfig.withTls(certChainPath, privateKeyPath, trustCertsPath);
    }

    @Bean
    public GrpcDataServiceDelegator grpcDataServiceDelegator(
        @NotNull GrpcConnectionConfig connectionConfig,
        @Value("${aipr.engine.host:localhost}") String engineHost,
        @Value("${aipr.engine.port:50051}") int enginePort) {

        log.info("Creating gRPC delegator for engine at {}:{} (tls={})",
                engineHost, enginePort, !connectionConfig.isUsePlaintext());

        var serverInstances = List.of(
            new GrpcDataServiceDelegator.ServerInstance(engineHost, enginePort, 1)
        );

        return new GrpcDataServiceDelegator(connectionConfig, serverInstances);
    }
}


