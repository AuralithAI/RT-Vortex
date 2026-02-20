package ai.aipr.server.llm;

import ai.aipr.server.dto.LLMResponse;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import okhttp3.*;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import jakarta.annotation.PostConstruct;
import java.io.IOException;
import java.time.Duration;
import java.util.List;
import java.util.Map;

/**
 * Client for calling LLM APIs.
 */
@Component
public class LLMClient {
    
    private static final Logger log = LoggerFactory.getLogger(LLMClient.class);
    private static final MediaType JSON = MediaType.parse("application/json");
    
    @Value("${aipr.llm.base-url}")
    private String baseUrl;
    
    @Value("${aipr.llm.api-key}")
    private String apiKey;
    
    @Value("${aipr.llm.model}")
    private String model;
    
    @Value("${aipr.llm.max-tokens}")
    private int maxTokens;
    
    @Value("${aipr.llm.temperature}")
    private double temperature;
    
    @Value("${aipr.llm.timeout-ms}")
    private int timeoutMs;
    
    private OkHttpClient httpClient;
    private ObjectMapper objectMapper;
    
    @PostConstruct
    public void init() {
        httpClient = new OkHttpClient.Builder()
                .connectTimeout(Duration.ofSeconds(30))
                .readTimeout(Duration.ofMillis(timeoutMs))
                .writeTimeout(Duration.ofSeconds(30))
                .build();
        
        objectMapper = new ObjectMapper();
        
        log.info("LLM client initialized: model={}, baseUrl={}", model, baseUrl);
    }
    
    /**
     * Complete a prompt using the LLM.
     */
    public LLMResponse complete(String prompt) {
        return complete(prompt, null);
    }
    
    /**
     * Complete a prompt with system message.
     */
    public LLMResponse complete(String prompt, String systemMessage) {
        log.debug("Calling LLM: model={}, promptLength={}", model, prompt.length());
        
        try {
            var messages = new java.util.ArrayList<Map<String, String>>();
            
            if (systemMessage != null && !systemMessage.isBlank()) {
                messages.add(Map.of("role", "system", "content", systemMessage));
            }
            
            messages.add(Map.of("role", "user", "content", prompt));
            
            var requestBody = Map.of(
                    "model", model,
                    "messages", messages,
                    "max_tokens", maxTokens,
                    "temperature", temperature,
                    "response_format", Map.of("type", "json_object")
            );
            
            var request = new Request.Builder()
                    .url(baseUrl + "/chat/completions")
                    .header("Authorization", "Bearer " + apiKey)
                    .header("Content-Type", "application/json")
                    .post(RequestBody.create(objectMapper.writeValueAsString(requestBody), JSON))
                    .build();
            
            try (var response = httpClient.newCall(request).execute()) {
                if (!response.isSuccessful()) {
                    String errorBody = response.body() != null ? response.body().string() : "No body";
                    log.error("LLM request failed: status={}, body={}", response.code(), errorBody);
                    throw new RuntimeException("LLM request failed: " + response.code());
                }
                
                var responseBody = objectMapper.readTree(response.body().string());
                return parseResponse(responseBody);
            }
            
        } catch (IOException e) {
            log.error("LLM request failed", e);
            throw new RuntimeException("LLM request failed: " + e.getMessage(), e);
        }
    }
    
    /**
     * Stream a completion (for longer responses).
     */
    public void streamComplete(String prompt, StreamCallback callback) {
        // TODO: Implement streaming for longer responses
        var response = complete(prompt);
        callback.onContent(response.content());
        callback.onComplete();
    }
    
    /**
     * Get the model being used.
     */
    public String getModel() {
        return model;
    }
    
    private LLMResponse parseResponse(JsonNode responseBody) {
        var choices = responseBody.get("choices");
        if (choices == null || choices.isEmpty()) {
            throw new RuntimeException("No choices in LLM response");
        }
        
        var message = choices.get(0).get("message");
        var content = message.get("content").asText();
        var finishReason = choices.get(0).get("finish_reason").asText();
        
        var usage = responseBody.get("usage");
        int promptTokens = usage != null ? usage.get("prompt_tokens").asInt() : 0;
        int completionTokens = usage != null ? usage.get("completion_tokens").asInt() : 0;
        
        return LLMResponse.builder()
                .content(content)
                .finishReason(finishReason)
                .promptTokens(promptTokens)
                .completionTokens(completionTokens)
                .tokensUsed(promptTokens + completionTokens)
                .model(model)
                .build();
    }
    
    /**
     * Callback interface for streaming responses.
     */
    public interface StreamCallback {
        void onContent(String content);
        void onComplete();
        default void onError(Exception e) {}
    }
}
