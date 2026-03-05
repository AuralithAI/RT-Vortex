package dev.rtvortex.sdk;

public class ValidationException extends RTVortexException {
    public ValidationException(String message, String body) {
        super(message, 422, body);
    }
}
