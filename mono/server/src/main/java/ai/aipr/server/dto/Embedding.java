package ai.aipr.server.dto;

import java.util.List;

/**
 * An embedding vector for a chunk.
 */
public record Embedding(
        String chunkId,
        List<Float> vector,
        String model,
        int dimensions
) {
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String chunkId;
        private List<Float> vector = List.of();
        private String model;
        private int dimensions;
        
        public Builder chunkId(String chunkId) { this.chunkId = chunkId; return this; }
        public Builder vector(List<Float> vector) { this.vector = vector; return this; }
        public Builder model(String model) { this.model = model; return this; }
        public Builder dimensions(int dimensions) { this.dimensions = dimensions; return this; }
        
        public Embedding build() {
            return new Embedding(chunkId, vector, model, dimensions);
        }
    }
}
