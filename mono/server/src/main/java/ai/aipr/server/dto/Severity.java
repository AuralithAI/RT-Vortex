package ai.aipr.server.dto;

/**
 * Severity level for review comments and heuristic findings.
 */
public enum Severity {
    ERROR("error", 4),
    WARNING("warning", 3),
    INFO("info", 2),
    SUGGESTION("suggestion", 1);
    
    private final String value;
    private final int priority;
    
    Severity(String value, int priority) {
        this.value = value;
        this.priority = priority;
    }
    
    public String getValue() {
        return value;
    }
    
    public int getPriority() {
        return priority;
    }
    
    public static Severity fromString(String text) {
        for (Severity s : Severity.values()) {
            if (s.value.equalsIgnoreCase(text) || s.name().equalsIgnoreCase(text)) {
                return s;
            }
        }
        return INFO;
    }
}
