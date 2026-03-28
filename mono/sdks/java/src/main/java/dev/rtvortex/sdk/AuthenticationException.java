package dev.rtvortex.sdk;

public class AuthenticationException extends RTVortexException {
    public AuthenticationException(String message, String body) {
        super(message, 401, body);
    }
}
