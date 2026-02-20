package ai.aipr.server.model;

/**
 * Page request for pagination.
 */
public class PageRequest {
    
    public static final PageRequest DEFAULT = new PageRequest(20, null);
    
    private final int size;
    private final String token;
    
    public PageRequest(int size, String token) {
        this.size = size;
        this.token = token;
    }
    
    public int getSize() { return size; }
    public String getToken() { return token; }
    
    public static PageRequest of(int size) {
        return new PageRequest(size, null);
    }
    
    public static PageRequest of(int size, String token) {
        return new PageRequest(size, token);
    }
}
