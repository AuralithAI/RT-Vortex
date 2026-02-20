package ai.aipr.server.model;

/**
 * Index status information.
 */
public class IndexStatusInfo {
    
    private final String repositoryId;
    private final boolean indexed;
    private final int totalFiles;
    private final int indexedFiles;
    private final int totalChunks;
    private final String lastCommit;
    private final String jobStatus;
    private final float jobProgress;
    
    public IndexStatusInfo(String repositoryId, boolean indexed, int totalFiles, 
                           int indexedFiles, int totalChunks, String lastCommit,
                           String jobStatus, float jobProgress) {
        this.repositoryId = repositoryId;
        this.indexed = indexed;
        this.totalFiles = totalFiles;
        this.indexedFiles = indexedFiles;
        this.totalChunks = totalChunks;
        this.lastCommit = lastCommit;
        this.jobStatus = jobStatus;
        this.jobProgress = jobProgress;
    }
    
    public String getRepositoryId() { return repositoryId; }
    public boolean isIndexed() { return indexed; }
    public int getTotalFiles() { return totalFiles; }
    public int getIndexedFiles() { return indexedFiles; }
    public int getTotalChunks() { return totalChunks; }
    public String getLastCommit() { return lastCommit; }
    public String getJobStatus() { return jobStatus; }
    public float getJobProgress() { return jobProgress; }
}
