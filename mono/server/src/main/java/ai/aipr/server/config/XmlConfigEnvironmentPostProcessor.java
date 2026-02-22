package ai.aipr.server.config;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.env.EnvironmentPostProcessor;
import org.springframework.core.Ordered;
import org.springframework.core.env.ConfigurableEnvironment;

/**
 * Registers {@link XmlConfigPropertySource} into Spring's environment
 * <b>before</b> any {@code @Value} resolution occurs.
 *
 * <p>The XML source is inserted with low priority so that:
 * <ol>
 *   <li>System properties ({@code -Dkey=value}) override XML</li>
 *   <li>Environment variables ({@code export KEY=value}) override XML</li>
 *   <li>XML values override any remaining defaults</li>
 * </ol>
 *
 * <p>Registered via {@code META-INF/spring.factories}.</p>
 */
public class XmlConfigEnvironmentPostProcessor implements EnvironmentPostProcessor, Ordered {

    /**
     * Run after default processors but before application context refresh.
     */
    @Override
    public int getOrder() {
        return Ordered.LOWEST_PRECEDENCE - 10;
    }

    @Override
    public void postProcessEnvironment(ConfigurableEnvironment environment,
                                       SpringApplication application) {
        try {
            XmlConfigPropertySource xmlSource = new XmlConfigPropertySource();
            // Add after systemProperties and systemEnvironment so env vars win
            environment.getPropertySources().addLast(xmlSource);
        } catch (Exception e) {
            // Log to stderr since logging may not be initialized yet
            System.err.println("Failed to load XML configuration: " + e.getMessage());
        }
    }
}

