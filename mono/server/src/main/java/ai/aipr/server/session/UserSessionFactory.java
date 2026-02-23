package ai.aipr.server.session;

import ai.aipr.server.grpc.GrpcDataServiceDelegator;
import ai.aipr.server.model.UserInfo;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;

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
    private final GrpcDataServiceDelegator delegator;

    @Autowired
    public UserSessionFactory(
            SessionManager sessionManager,
            GrpcDataServiceDelegator delegator) {

        this.sessionManager = sessionManager;
        this.delegator = delegator;
    }

    /**
     * Create a session for a platform OAuth token.
     *
     * @param platform     The platform (GitHub, gitlab, bitbucket)
     * @param accessToken  The OAuth access token from the platform
     * @return A new user session
     */
    public IUserSession createSession(String platform, String accessToken) {
        return createSession(platform, accessToken, "unknown", "unknown");
    }

    /**
     * Create a session with client information.
     *
     * @param platform      The platform (GitHub, gitlab, bitbucket)
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
            user.id(), platform, clientType, clientVersion
        );

        // Create remote session wrapper using shared delegator
        IUserSessionRemote remote = new UserSessionRemote(
            validatedSession.sessionId(),
            validatedSession.grpcChannelId(),
            delegator
        );

        // Create and return the session
        return new UserSession(
            validatedSession.sessionId(),
            validatedSession.sessionId(), // session token
            user,
            validatedSession.expiresAt(),
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
        UserInfo user = UserInfo.builder()
            .id(validatedSession.userId())
            .platform("unknown")
            .username(validatedSession.username())
            .build();

        // Create remote session wrapper using shared delegator
        IUserSessionRemote remote = new UserSessionRemote(
            sessionToken,
            validatedSession.grpcChannelId(),
            delegator
        );

        return new UserSession(
            validatedSession.sessionId(),
            sessionToken,
            user,
            validatedSession.expiresAt(),
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

        return UserInfo.builder()
            .id(java.util.UUID.randomUUID().toString())
            .platform(platform)
            .username("authenticated_user")
            .email("user@example.com")
            .displayName("Authenticated User")
            .build();
    }
}
