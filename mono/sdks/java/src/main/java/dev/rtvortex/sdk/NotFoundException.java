package dev.rtvortex.sdk;

public class NotFoundException extends RTVortexException {
    public NotFoundException(String message, String body) {
        super(message, 404, body);
    }
}
