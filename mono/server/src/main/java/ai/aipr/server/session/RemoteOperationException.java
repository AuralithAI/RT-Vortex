package ai.aipr.server.session;

/**
 * Exception thrown when a remote operation fails.
 */
public class RemoteOperationException extends RuntimeException {
    
    public RemoteOperationException(String message) {
        super(message);
    }
    
    public RemoteOperationException(String message, Throwable cause) {
        super(message, cause);
    }
}
