package ai.aipr.server.model;

/**
 * Review comment from analysis.
 */
public class ReviewComment {
    
    private final String id;
    private final String filePath;
    private final int line;
    private final int endLine;
    private final String severity;
    private final String category;
    private final String message;
    private final String suggestion;
    private final float confidence;
    
    public ReviewComment(String id, String filePath, int line, int endLine,
                         String severity, String category, String message,
                         String suggestion, float confidence) {
        this.id = id;
        this.filePath = filePath;
        this.line = line;
        this.endLine = endLine;
        this.severity = severity;
        this.category = category;
        this.message = message;
        this.suggestion = suggestion;
        this.confidence = confidence;
    }
    
    public String getId() { return id; }
    public String getFilePath() { return filePath; }
    public int getLine() { return line; }
    public int getEndLine() { return endLine; }
    public String getSeverity() { return severity; }
    public String getCategory() { return category; }
    public String getMessage() { return message; }
    public String getSuggestion() { return suggestion; }
    public float getConfidence() { return confidence; }
}
