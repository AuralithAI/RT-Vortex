package ai.aipr.server.model;

import java.util.List;

/**
 * Paginated result wrapper.
 */
public class PagedResult<T> {
    
    private final List<T> items;
    private final String nextPageToken;
    private final int totalCount;
    private final int page;
    private final int size;
    
    public PagedResult(List<T> items, String nextPageToken, int totalCount) {
        this.items = items;
        this.nextPageToken = nextPageToken;
        this.totalCount = totalCount;
        this.page = 0;
        this.size = items.size();
    }
    
    public PagedResult(List<T> items, int totalCount, int page, int size) {
        this.items = items;
        this.totalCount = totalCount;
        this.page = page;
        this.size = size;
        this.nextPageToken = (page + 1) * size < totalCount ? String.valueOf(page + 1) : null;
    }
    
    public List<T> getItems() { return items; }
    public String getNextPageToken() { return nextPageToken; }
    public int getTotalCount() { return totalCount; }
    public int getPage() { return page; }
    public int getSize() { return size; }
    
    public boolean hasNextPage() {
        return nextPageToken != null && !nextPageToken.isEmpty();
    }
}
