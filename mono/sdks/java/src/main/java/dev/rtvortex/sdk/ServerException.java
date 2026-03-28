package dev.rtvortex.sdk;

public class ServerException extends RTVortexException {
    public ServerException(String message, int statusCode, String body) {
        super(message, statusCode, body);
    }
}
