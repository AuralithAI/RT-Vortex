package ai.aipr.server;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.cache.annotation.EnableCaching;
import org.springframework.scheduling.annotation.EnableAsync;
import org.springframework.scheduling.annotation.EnableScheduling;

/**
 * AI PR Reviewer Server
 * 
 * Main application entry point. Provides REST API for:
 * - PR review requests
 * - Repository indexing
 * - Configuration management
 * - Platform integrations
 */
@SpringBootApplication
@EnableCaching
@EnableAsync
@EnableScheduling
public class Application {

    public static void main(String[] args) {
        SpringApplication.run(Application.class, args);
    }
}
