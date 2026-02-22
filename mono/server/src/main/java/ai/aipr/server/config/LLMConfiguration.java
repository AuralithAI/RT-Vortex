package ai.aipr.server.config;

import ai.aipr.server.service.LLMService.LLMProvidersConfig;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

/**
 * Configuration for LLM providers.
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
