package dev.rtvortex.sdk;

/**
 * Base exception for all RTVortex SDK errors.
 */
public class RTVortexException extends RuntimeException {

    private final int statusCode;
    private final String body;

    public RTVortexException(String message, int statusCode, String body) {
        super(message);
        this.statusCode = statusCode;
        this.body = body;
    }

    public RTVortexException(String message, int statusCode) {
        this(message, statusCode, null);
    }

    public int getStatusCode() {
        return statusCode;
    }

    public String getBody() {
        return body;
    }
}
