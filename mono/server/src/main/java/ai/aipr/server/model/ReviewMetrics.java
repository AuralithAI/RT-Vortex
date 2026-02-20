package ai.aipr.server.model;

/**
 * Review metrics.
 */
public class ReviewMetrics {
    
    private final int filesAnalyzed;
    private final int linesAdded;
    private final int linesRemoved;
    private final int totalFindings;
    private final int tokensUsed;
    private final int latencyMs;
    
    public ReviewMetrics(int filesAnalyzed, int linesAdded, int linesRemoved,
                         int totalFindings, int tokensUsed, int latencyMs) {
        this.filesAnalyzed = filesAnalyzed;
        this.linesAdded = linesAdded;
        this.linesRemoved = linesRemoved;
        this.totalFindings = totalFindings;
        this.tokensUsed = tokensUsed;
        this.latencyMs = latencyMs;
    }
    
    public int getFilesAnalyzed() { return filesAnalyzed; }
    public int getLinesAdded() { return linesAdded; }
    public int getLinesRemoved() { return linesRemoved; }
    public int getTotalFindings() { return totalFindings; }
    public int getTokensUsed() { return tokensUsed; }
    public int getLatencyMs() { return latencyMs; }
}
