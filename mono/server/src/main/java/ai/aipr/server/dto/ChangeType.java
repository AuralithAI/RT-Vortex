package ai.aipr.server.dto;

/**
 * Type of change for a file in a diff.
 */
public enum ChangeType {
    ADDED("A"),
    MODIFIED("M"),
    DELETED("D"),
    RENAMED("R"),
    COPIED("C"),
    UNMERGED("U"),
    UNKNOWN("X");
    
    private final String code;
    
    ChangeType(String code) {
        this.code = code;
    }
    
    public String getCode() {
        return code;
    }
    
    public static ChangeType fromCode(String code) {
        if (code == null || code.isEmpty()) {
            return UNKNOWN;
        }
        
        String normalized = code.toUpperCase().substring(0, 1);
        for (ChangeType type : ChangeType.values()) {
            if (type.code.equals(normalized)) {
                return type;
            }
        }
        return UNKNOWN;
    }
    
    public static ChangeType fromString(String text) {
        if (text == null || text.isEmpty()) {
            return UNKNOWN;
        }
        
        try {
            return ChangeType.valueOf(text.toUpperCase());
        } catch (IllegalArgumentException e) {
            return fromCode(text);
        }
    }
}
