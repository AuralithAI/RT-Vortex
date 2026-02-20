package ai.aipr.server.model;

import java.util.List;

/**
 * Paginated result wrapper.
 */
public class PagedResult<T> {
    
    private final List<T> items;
    private final String nextPageToken;
    private final int totalCount;
    
    public PagedResult(List<T> items, String nextPageToken, int totalCount) {
        this.items = items;
        this.nextPageToken = nextPageToken;
        this.totalCount = totalCount;
    }
    
    public List<T> getItems() { return items; }
    public String getNextPageToken() { return nextPageToken; }
    public int getTotalCount() { return totalCount; }
    
    public boolean hasNextPage() {
        return nextPageToken != null && !nextPageToken.isEmpty();
    }
}
