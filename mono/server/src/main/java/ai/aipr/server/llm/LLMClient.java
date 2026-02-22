package ai.aipr.server.llm;

import ai.aipr.server.dto.LLMResponse;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import okhttp3.Call;
import okhttp3.Callback;
import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import jakarta.annotation.PostConstruct;
import java.io.IOException;
import java.time.Duration;
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
    public LLMResponse complete(@NotNull String prompt, String systemMessage) {
        log.debug("Calling LLM: model={}, promptLength={}", model, prompt.length());

        long startNanos = System.nanoTime();

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
                long latencyMs = (System.nanoTime() - startNanos) / 1_000_000;

                if (!response.isSuccessful()) {
                    String errorBody = response.body() != null ? response.body().string() : "No body";
                    log.error("LLM request failed: status={}, body={}", response.code(), errorBody);
                    throw new RuntimeException("LLM request failed: " + response.code());
                }

                assert response.body() != null;
                var responseBody = objectMapper.readTree(response.body().string());
                return parseResponse(responseBody, latencyMs);
            }

        } catch (IOException e) {
            log.error("LLM request failed", e);
            throw new RuntimeException("LLM request failed: " + e.getMessage(), e);
        }
    }

    /**
     * Stream a completion using SSE (Server-Sent Events).
     * Calls the OpenAI-compatible streaming endpoint with {@code stream: true}.
     * Each delta token is delivered to the callback as it arrives.
     *
     * @param prompt   the user prompt
     * @param callback receives streamed content chunks, completion, and errors
     */
    public void streamComplete(String prompt, StreamCallback callback) {
        streamComplete(prompt, null, callback);
    }

    /**
     * Stream a completion with system message using SSE.
     *
     * @param prompt        the user prompt
     * @param systemMessage optional system message (null to skip)
     * @param callback      receives streamed content chunks, completion, and errors
     */
    public void streamComplete(@NotNull String prompt, String systemMessage, StreamCallback callback) {
        log.debug("Streaming LLM: model={}, promptLength={}", model, prompt.length());

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
                    "stream", true
            );

            var request = new Request.Builder()
                    .url(baseUrl + "/chat/completions")
                    .header("Authorization", "Bearer " + apiKey)
                    .header("Content-Type", "application/json")
                    .header("Accept", "text/event-stream")
                    .post(RequestBody.create(objectMapper.writeValueAsString(requestBody), JSON))
                    .build();

            httpClient.newCall(request).enqueue(new Callback() {
                @Override
                public void onFailure(@NotNull Call call, @NotNull IOException e) {
                    log.error("LLM stream request failed", e);
                    callback.onError(e);
                }

                @Override
                public void onResponse(@NotNull Call call, @NotNull Response response) {
                    try {
                        if (!response.isSuccessful()) {
                            String errorBody = response.body() != null ? response.body().string() : "No body";
                            var ex = new RuntimeException("LLM stream failed: " + response.code() + " " + errorBody);
                            log.error("LLM stream request failed: status={}", response.code());
                            callback.onError(ex);
                            return;
                        }

                        if (response.body() == null) {
                            callback.onError(new RuntimeException("Empty response body from LLM stream"));
                            return;
                        }

                        // Read SSE events line by line
                        try (var source = response.body().source()) {
                            while (!source.exhausted()) {
                                String line = source.readUtf8LineStrict();

                                if (line.isEmpty() || line.startsWith(":")) {
                                    // Empty line (event boundary) or SSE comment — skip
                                    continue;
                                }

                                if (!line.startsWith("data: ")) {
                                    continue;
                                }

                                String data = line.substring(6).trim();

                                // "[DONE]" signals the end of the stream
                                if ("[DONE]".equals(data)) {
                                    callback.onComplete();
                                    return;
                                }

                                // Parse the delta JSON
                                var node = objectMapper.readTree(data);
                                var choices = node.get("choices");
                                if (choices != null && !choices.isEmpty()) {
                                    var delta = choices.get(0).get("delta");
                                    if (delta != null && delta.has("content")) {
                                        String content = delta.get("content").asText("");
                                        if (!content.isEmpty()) {
                                            callback.onContent(content);
                                        }
                                    }
                                }
                            }
                        }

                        // If we exit the loop without [DONE], still signal completion
                        callback.onComplete();

                    } catch (Exception e) {
                        log.error("Error processing LLM stream", e);
                        callback.onError(e);
                    }
                }
            });

        } catch (Exception e) {
            log.error("Failed to initiate LLM stream", e);
            callback.onError(e);
        }
    }

    /**
     * Get the model being used.
     */
    public String getModel() {
        return model;
    }

    private LLMResponse parseResponse(@NotNull JsonNode responseBody, long latencyMs) {
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
        int totalTokens = usage != null && usage.has("total_tokens")
                ? usage.get("total_tokens").asInt()
                : promptTokens + completionTokens;

        return LLMResponse.builder()
                .content(content)
                .finishReason(finishReason)
                .promptTokens(promptTokens)
                .completionTokens(completionTokens)
                .totalTokens(totalTokens)
                .model(model)
                .latencyMs(latencyMs)
                .build();
    }

    /**
     * Callback interface for streaming responses.
     * Implementations must handle {@link #onContent} and {@link #onComplete}.
     * Override {@link #onError} to handle errors (default logs and rethrows).
     */
    public interface StreamCallback {
        /** Called for each content delta received from the LLM. */
        void onContent(String content);

        /** Called when the stream is fully complete. */
        void onComplete();

        /**
         * Called when an error occurs during streaming.
         * Default implementation logs the error and wraps it as a RuntimeException.
         */
        default void onError(Exception e) {
            LoggerFactory.getLogger(StreamCallback.class)
                    .error("LLM stream error (unhandled by callback)", e);
            if (e instanceof RuntimeException re) throw re;
            throw new RuntimeException("LLM stream error", e);
        }
    }
}
