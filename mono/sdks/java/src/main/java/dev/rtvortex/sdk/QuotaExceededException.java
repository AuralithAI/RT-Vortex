package dev.rtvortex.sdk;

public class QuotaExceededException extends RTVortexException {
    public QuotaExceededException(String message, int statusCode, String body) {
        super(message, statusCode, body);
    }
}
