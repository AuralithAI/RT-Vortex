package ai.aipr.server.task;

import jakarta.annotation.PostConstruct;
import jakarta.annotation.PreDestroy;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.concurrent.ThreadPoolTaskScheduler;
import org.springframework.scheduling.support.CronTrigger;
import org.springframework.stereotype.Component;

import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ScheduledFuture;

/**
 * Central scheduler that discovers all {@link IBackgroundTask} beans and
 * schedules them according to their cron expressions.
 *
 * <p>Runs inside the Java server process — no external Quartz server needed.
 * All tasks execute with superuser privileges (no user session context).</p>
 *
 * <p>When the C++ engine and Java server are split into separate microservices,
 * this scheduler can be extracted into its own service that connects to the
 * Java server over gRPC to trigger maintenance operations remotely.</p>
 */
@Component
public class BackgroundTaskScheduler {

    private static final Logger log = LoggerFactory.getLogger(BackgroundTaskScheduler.class);

    private final List<IBackgroundTask> tasks;
    private final Map<String, ScheduledFuture<?>> scheduledTasks = new ConcurrentHashMap<>();
    private final Map<String, Boolean> runningTasks = new ConcurrentHashMap<>();
    private ThreadPoolTaskScheduler executor;

    public BackgroundTaskScheduler(List<IBackgroundTask> tasks) {
        this.tasks = tasks;
    }

    @PostConstruct
    public void start() {
        executor = new ThreadPoolTaskScheduler();
        executor.setPoolSize(Math.max(2, tasks.size()));
        executor.setThreadNamePrefix("aipr-task-");
        executor.setErrorHandler(t -> log.error("Unhandled error in background task", t));
        executor.initialize();

        int scheduled = 0;
        for (IBackgroundTask task : tasks) {
            if (!task.isEnabled()) {
                log.info("Task [{}] is disabled, skipping", task.name());
                continue;
            }

            try {
                CronTrigger trigger = new CronTrigger(task.cronExpression());
                ScheduledFuture<?> future = executor.schedule(
                    () -> executeTask(task), trigger
                );
                scheduledTasks.put(task.name(), future);
                scheduled++;
                log.info("Scheduled task [{}] with cron [{}]", task.name(), task.cronExpression());
            } catch (IllegalArgumentException e) {
                log.error("Invalid cron expression for task [{}]: {}", task.name(), task.cronExpression());
            }
        }

        log.info("BackgroundTaskScheduler started: {}/{} tasks scheduled", scheduled, tasks.size());
    }

    @PreDestroy
    public void shutdown() {
        log.info("Shutting down BackgroundTaskScheduler...");
        scheduledTasks.values().forEach(f -> f.cancel(false));
        if (executor != null) {
            executor.shutdown();
        }
    }

    private void executeTask(@NotNull IBackgroundTask task) {
        String name = task.name();

        // Skip if already running and concurrent execution not allowed
        if (!task.allowConcurrentExecution()) {
            Boolean alreadyRunning = runningTasks.putIfAbsent(name, true);
            if (alreadyRunning != null) {
                log.debug("Task [{}] still running from previous invocation, skipping", name);
                return;
            }
        } else {
            runningTasks.put(name, true);
        }

        TaskContext context = new TaskContext(name);
        try {
            TaskResult result = task.execute(context);
            long elapsed = context.elapsedMs();

            switch (result.status()) {
                case SUCCESS -> log.info("Task [{}] completed in {}ms: {} (items={})",
                        name, elapsed, result.message(), result.itemsProcessed());
                case PARTIAL -> log.warn("Task [{}] partial in {}ms: {} (items={})",
                        name, elapsed, result.message(), result.itemsProcessed());
                case SKIPPED -> log.debug("Task [{}] skipped: {}", name, result.message());
                case FAILED -> log.error("Task [{}] failed in {}ms: {}", name, elapsed, result.message());
            }
        } catch (Exception e) {
            log.error("Task [{}] threw exception after {}ms", name, context.elapsedMs(), e);
        } finally {
            runningTasks.remove(name);
        }
    }
}
