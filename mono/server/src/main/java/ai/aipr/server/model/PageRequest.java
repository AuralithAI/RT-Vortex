package ai.aipr.server.model;

/**
 * Page request for pagination.
 */
public record PageRequest(
        int page,
        int size,
        String token
) {
    public static final PageRequest DEFAULT = new PageRequest(0, 20, null);
    
    public PageRequest(int size, String token) {
        this(0, size, token);
    }
    
    public static PageRequest of(int size) {
        return new PageRequest(0, size, null);
    }
    
    public static PageRequest of(int page, int size) {
        return new PageRequest(page, size, null);
    }
    
    public static PageRequest of(int size, String token) {
        return new PageRequest(0, size, token);
    }
}
