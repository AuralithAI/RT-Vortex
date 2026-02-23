package ai.aipr.server.task;

import ai.aipr.server.llm.LLMProviderManager;
import org.springframework.stereotype.Component;

/**
 * Periodic health check of all LLM providers.
 * Runs every 60 seconds to detect provider failures and trigger failover.
 */
@Component
public class LLMHealthCheckBackgroundTask implements IBackgroundTask {

    private final LLMProviderManager providerManager;

    public LLMHealthCheckBackgroundTask(LLMProviderManager providerManager) {
        this.providerManager = providerManager;
    }

    @Override
    public String name() {
        return "llm-health-check";
    }

    @Override
    public String cronExpression() {
        return "0 * * * * ?";  // Every minute
    }

    @Override
    public TaskResult execute(TaskContext context) {
        try {
            providerManager.performHealthCheck();

            String active = providerManager.getActiveProvider();
            boolean activeHealthy = providerManager.isProviderHealthy(active);
            long healthyCount = providerManager.getAllProviders().stream()
                    .filter(LLMProviderManager.ProviderStatus::healthy)
                    .count();

            if (!activeHealthy) {
                return TaskResult.failed("Active provider '" + active + "' is unhealthy. " +
                        healthyCount + " healthy provider(s) available.");
            }

            return TaskResult.success((int) healthyCount,
                    "Active: " + active + ", healthy: " + healthyCount + "/" +
                    providerManager.getAllProviders().size());
        } catch (Exception e) {
            return TaskResult.failed("Health check error: " + e.getMessage());
        }
    }
}

