package ai.auralith.aipr;

import ai.auralith.aipr.model.*;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import okhttp3.*;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.time.Duration;
import java.util.Objects;
import java.util.concurrent.CompletableFuture;

/**
 * Main client for interacting with the AI-PR-Reviewer API.
 * 
 * <p>Example usage:</p>
 * <pre>{@code
 * AIPRClient client = AIPRClient.builder()
 *     .baseUrl("https://api.aipr.example.com")
 *     .apiKey("your-api-key")
 *     .build();
 *     
 * ReviewResponse response = client.review(ReviewRequest.builder()
 *     .repositoryUrl("https://github.com/owner/repo")
 *     .pullRequestId(123)
 *     .build());
 * }</pre>
 */
public class AIPRClient implements AutoCloseable {
    private static final Logger log = LoggerFactory.getLogger(AIPRClient.class);
    private static final MediaType JSON = MediaType.parse("application/json");
    
    private final String baseUrl;
    private final OkHttpClient httpClient;
    private final ObjectMapper objectMapper;
    
    private AIPRClient(Builder builder) {
        this.baseUrl = builder.baseUrl.endsWith("/") 
            ? builder.baseUrl.substring(0, builder.baseUrl.length() - 1) 
            : builder.baseUrl;
        
        this.objectMapper = new ObjectMapper()
            .registerModule(new JavaTimeModule())
            .configure(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES, false);
        
        OkHttpClient.Builder clientBuilder = new OkHttpClient.Builder()
            .connectTimeout(builder.connectTimeout)
            .readTimeout(builder.readTimeout)
            .writeTimeout(builder.writeTimeout);
        
        // Add authentication interceptor
        if (builder.apiKey != null && !builder.apiKey.isEmpty()) {
            clientBuilder.addInterceptor(chain -> {
                Request original = chain.request();
                Request.Builder requestBuilder = original.newBuilder()
                    .header("Authorization", "Bearer " + builder.apiKey)
                    .header("Content-Type", "application/json")
                    .header("Accept", "application/json")
                    .header("User-Agent", "aipr-java-sdk/0.1.0");
                return chain.proceed(requestBuilder.build());
            });
        }
        
        this.httpClient = clientBuilder.build();
    }
    
    /**
     * Creates a new builder for AIPRClient.
     */
    public static Builder builder() {
        return new Builder();
    }
    
    /**
     * Submit a pull request for review.
     * 
     * @param request The review request
     * @return The review response with comments and metadata
     * @throws AIPRException if the request fails
     */
    public ReviewResponse review(ReviewRequest request) throws AIPRException {
        return executePost("/api/v1/reviews", request, ReviewResponse.class);
    }
    
    /**
     * Submit a pull request for review asynchronously.
     * 
     * @param request The review request
     * @return A CompletableFuture containing the review response
     */
    public CompletableFuture<ReviewResponse> reviewAsync(ReviewRequest request) {
        return executePostAsync("/api/v1/reviews", request, ReviewResponse.class);
    }
    
    /**
     * Get the status of a review by ID.
     * 
     * @param reviewId The review ID
     * @return The review response
     * @throws AIPRException if the request fails
     */
    public ReviewResponse getReview(String reviewId) throws AIPRException {
        return executeGet("/api/v1/reviews/" + reviewId, ReviewResponse.class);
    }
    
    /**
     * Start indexing a repository.
     * 
     * @param request The index request
     * @return The index job response
     * @throws AIPRException if the request fails
     */
    public IndexResponse index(IndexRequest request) throws AIPRException {
        return executePost("/api/v1/index", request, IndexResponse.class);
    }
    
    /**
     * Get the status of an indexing job.
     * 
     * @param jobId The job ID
     * @return The index job response
     * @throws AIPRException if the request fails
     */
    public IndexResponse getIndexStatus(String jobId) throws AIPRException {
        return executeGet("/api/v1/index/" + jobId, IndexResponse.class);
    }
    
    /**
     * Check the health of the API server.
     * 
     * @return true if the server is healthy
     */
    public boolean isHealthy() {
        try {
            Request request = new Request.Builder()
                .url(baseUrl + "/actuator/health")
                .get()
                .build();
            
            try (Response response = httpClient.newCall(request).execute()) {
                return response.isSuccessful();
            }
        } catch (IOException e) {
            log.warn("Health check failed", e);
            return false;
        }
    }
    
    private <T> T executeGet(String path, Class<T> responseType) throws AIPRException {
        Request request = new Request.Builder()
            .url(baseUrl + path)
            .get()
            .build();
        
        return execute(request, responseType);
    }
    
    private <T> T executePost(String path, Object body, Class<T> responseType) throws AIPRException {
        try {
            String json = objectMapper.writeValueAsString(body);
            RequestBody requestBody = RequestBody.create(json, JSON);
            
            Request request = new Request.Builder()
                .url(baseUrl + path)
                .post(requestBody)
                .build();
            
            return execute(request, responseType);
        } catch (IOException e) {
            throw new AIPRException("Failed to serialize request", e);
        }
    }
    
    private <T> T execute(Request request, Class<T> responseType) throws AIPRException {
        try (Response response = httpClient.newCall(request).execute()) {
            String responseBody = response.body() != null ? response.body().string() : "";
            
            if (!response.isSuccessful()) {
                throw new AIPRException(
                    String.format("API request failed: %d %s - %s", 
                        response.code(), response.message(), responseBody),
                    response.code()
                );
            }
            
            return objectMapper.readValue(responseBody, responseType);
        } catch (IOException e) {
            throw new AIPRException("Failed to execute request", e);
        }
    }
    
    private <T> CompletableFuture<T> executePostAsync(String path, Object body, Class<T> responseType) {
        CompletableFuture<T> future = new CompletableFuture<>();
        
        try {
            String json = objectMapper.writeValueAsString(body);
            RequestBody requestBody = RequestBody.create(json, JSON);
            
            Request request = new Request.Builder()
                .url(baseUrl + path)
                .post(requestBody)
                .build();
            
            httpClient.newCall(request).enqueue(new Callback() {
                @Override
                public void onFailure(Call call, IOException e) {
                    future.completeExceptionally(new AIPRException("Request failed", e));
                }
                
                @Override
                public void onResponse(Call call, Response response) throws IOException {
                    try (response) {
                        String responseBody = response.body() != null ? response.body().string() : "";
                        
                        if (!response.isSuccessful()) {
                            future.completeExceptionally(new AIPRException(
                                String.format("API request failed: %d %s", response.code(), response.message()),
                                response.code()
                            ));
                            return;
                        }
                        
                        T result = objectMapper.readValue(responseBody, responseType);
                        future.complete(result);
                    } catch (Exception e) {
                        future.completeExceptionally(new AIPRException("Failed to parse response", e));
                    }
                }
            });
        } catch (IOException e) {
            future.completeExceptionally(new AIPRException("Failed to serialize request", e));
        }
        
        return future;
    }
    
    @Override
    public void close() {
        httpClient.dispatcher().executorService().shutdown();
        httpClient.connectionPool().evictAll();
    }
    
    /**
     * Builder for AIPRClient.
     */
    public static class Builder {
        private String baseUrl = "http://localhost:8080";
        private String apiKey;
        private Duration connectTimeout = Duration.ofSeconds(30);
        private Duration readTimeout = Duration.ofMinutes(5);
        private Duration writeTimeout = Duration.ofSeconds(30);
        
        /**
         * Set the base URL of the AI-PR-Reviewer API.
         */
        public Builder baseUrl(String baseUrl) {
            this.baseUrl = Objects.requireNonNull(baseUrl, "baseUrl must not be null");
            return this;
        }
        
        /**
         * Set the API key for authentication.
         */
        public Builder apiKey(String apiKey) {
            this.apiKey = apiKey;
            return this;
        }
        
        /**
         * Set the connection timeout.
         */
        public Builder connectTimeout(Duration connectTimeout) {
            this.connectTimeout = Objects.requireNonNull(connectTimeout);
            return this;
        }
        
        /**
         * Set the read timeout.
         */
        public Builder readTimeout(Duration readTimeout) {
            this.readTimeout = Objects.requireNonNull(readTimeout);
            return this;
        }
        
        /**
         * Set the write timeout.
         */
        public Builder writeTimeout(Duration writeTimeout) {
            this.writeTimeout = Objects.requireNonNull(writeTimeout);
            return this;
        }
        
        /**
         * Build the AIPRClient instance.
         */
        public AIPRClient build() {
            return new AIPRClient(this);
        }
    }
}
