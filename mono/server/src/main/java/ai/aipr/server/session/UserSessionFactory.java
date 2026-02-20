package ai.aipr.server.session;

import ai.aipr.server.grpc.GrpcConnectionConfig;
import ai.aipr.server.grpc.GrpcDataServiceDelegator;
import ai.aipr.server.model.UserInfo;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import java.util.List;

/**
 * Factory for creating user sessions.
 * 
 * <p>This factory handles the creation of {@link IUserSession} instances,
 * managing the connection to the gRPC server and session lifecycle.</p>
 */
@Component
public class UserSessionFactory {

    private static final Logger log = LoggerFactory.getLogger(UserSessionFactory.class);

    private final SessionManager sessionManager;
    private final GrpcConnectionConfig grpcConfig;
    private final List<GrpcDataServiceDelegator.ServerInstance> serverInstances;

    @Autowired
    public UserSessionFactory(
            SessionManager sessionManager,
            @Value("${aipr.engine.host:localhost}") String engineHost,
            @Value("${aipr.engine.port:50051}") int enginePort,
            @Value("${grpc.server.port:9090}") int grpcServerPort) {
        
        this.sessionManager = sessionManager;
        this.grpcConfig = GrpcConnectionConfig.defaults();
        
        // Configure server instances
        // For SDK clients, they connect to the Java gRPC server (port 9090)
        // The Java server then connects to C++ engine (port 50051)
        this.serverInstances = List.of(
            new GrpcDataServiceDelegator.ServerInstance("localhost", grpcServerPort, 1)
        );
    }

    /**
     * Create a session for a platform OAuth token.
     *
     * @param platform     The platform (github, gitlab, bitbucket)
     * @param accessToken  The OAuth access token from the platform
     * @return A new user session
     */
    public IUserSession createSession(String platform, String accessToken) {
        return createSession(platform, accessToken, "unknown", "unknown");
    }

    /**
     * Create a session with client information.
     *
     * @param platform      The platform (github, gitlab, bitbucket)
     * @param accessToken   The OAuth access token from the platform
     * @param clientType    The client type (sdk_java, sdk_python, cli, web)
     * @param clientVersion The client version
     * @return A new user session
     */
    public IUserSession createSession(String platform, String accessToken,
                                       String clientType, String clientVersion) {
        // Authenticate with platform and get/create user
        UserInfo user = authenticateWithPlatform(platform, accessToken);
        
        // Create session in database
        SessionManager.ValidatedSession validatedSession = sessionManager.createSession(
            user.getId(), platform, clientType, clientVersion
        );
        
        // Create gRPC delegator for this session
        GrpcDataServiceDelegator delegator = new GrpcDataServiceDelegator(
            grpcConfig, serverInstances
        );
        
        // Create remote session wrapper
        IUserSessionRemote remote = new UserSessionRemote(
            validatedSession.getSessionId(), // Using session ID as token for simplicity
            validatedSession.getGrpcChannelId(),
            delegator
        );
        
        // Create and return the session
        return new UserSession(
            validatedSession.getSessionId(),
            validatedSession.getSessionId(), // session token
            user,
            validatedSession.getExpiresAt(),
            remote
        );
    }

    /**
     * Resume an existing session from a session token.
     *
     * @param sessionToken The session token
     * @return The resumed session, or null if invalid
     */
    public IUserSession resumeSession(String sessionToken) {
        SessionManager.ValidatedSession validatedSession = sessionManager.validateSession(sessionToken);
        if (validatedSession == null) {
            return null;
        }
        
        // Create user info from validated session
        UserInfo user = new UserInfo(
            validatedSession.getUserId(),
            "unknown", // We could fetch this from DB
            validatedSession.getUsername(),
            null, null, null
        );
        
        // Create gRPC delegator
        GrpcDataServiceDelegator delegator = new GrpcDataServiceDelegator(
            grpcConfig, serverInstances
        );
        
        // Create remote session wrapper
        IUserSessionRemote remote = new UserSessionRemote(
            sessionToken,
            validatedSession.getGrpcChannelId(),
            delegator
        );
        
        return new UserSession(
            validatedSession.getSessionId(),
            sessionToken,
            user,
            validatedSession.getExpiresAt(),
            remote
        );
    }

    /**
     * Authenticate with a platform and get or create user.
     */
    private UserInfo authenticateWithPlatform(String platform, String accessToken) {
        // TODO: Implement actual OAuth validation with GitHub/GitLab/Bitbucket
        // For now, return a placeholder
        log.info("Authenticating with platform: {}", platform);
        
        // This would:
        // 1. Call platform API to validate token and get user info
        // 2. Create or update user in database
        // 3. Return UserInfo
        
        return new UserInfo(
            java.util.UUID.randomUUID().toString(),
            platform,
            "authenticated_user",
            "user@example.com",
            "Authenticated User",
            null
        );
    }
}
