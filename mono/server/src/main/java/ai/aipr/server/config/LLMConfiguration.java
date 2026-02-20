package ai.aipr.server.config;

import ai.aipr.server.model.LLMProviderInfo;
import ai.aipr.server.service.LLMService.LLMProvidersConfig;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

/**
 * Configuration for LLM providers.
 * 
 * <p>Configure LLM providers in application.yml:</p>
 * <pre>
 * aipr:
 *   llm:
 *     providers:
 *       - id: openai
 *         name: OpenAI
 *         description: OpenAI GPT models for code review
 *         provider-type: openai
 *         supports-streaming: true
 *         available-models:
 *           - gpt-4-turbo-preview
 *           - gpt-4
 *           - gpt-3.5-turbo
 *         is-default: true
 *       - id: anthropic
 *         name: Anthropic
 *         description: Anthropic Claude models for code review
 *         provider-type: anthropic
 *         supports-streaming: true
 *         available-models:
 *           - claude-3-opus
 *           - claude-3-sonnet
 *           - claude-3-haiku
 *       - id: ollama
 *         name: Ollama (Local)
 *         description: Local LLM models via Ollama
 *         provider-type: ollama
 *         supports-streaming: false
 *         available-models:
 *           - llama3
 *           - codellama
 *           - mistral
 * </pre>
 */
@Configuration
@EnableConfigurationProperties
public class LLMConfiguration {
    
    @Bean
    @ConfigurationProperties(prefix = "aipr.llm")
    public LLMProvidersConfig llmProvidersConfig() {
        return new LLMProvidersConfig();
    }
}
