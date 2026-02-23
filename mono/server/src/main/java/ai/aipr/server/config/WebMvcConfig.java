package ai.aipr.server.config;

import ai.aipr.server.interceptor.RateLimitInterceptor;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;
import org.springframework.beans.factory.ObjectProvider;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.servlet.config.annotation.InterceptorRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

/**
 * Spring MVC configuration.
 *
 * <p>Registers the {@link RateLimitInterceptor} when Redis is enabled.
 * Without Redis the interceptor bean is not created (it is
 * {@code @ConditionalOnBean(RedisConfig.class)}), so this configurer
 * gracefully becomes a no-op.</p>
 */
@Configuration
public class WebMvcConfig implements WebMvcConfigurer {

    @Nullable
    private final RateLimitInterceptor rateLimitInterceptor;

    public WebMvcConfig(@NotNull ObjectProvider<RateLimitInterceptor> interceptorProvider) {
        this.rateLimitInterceptor = interceptorProvider.getIfAvailable();
    }

    @Override
    public void addInterceptors(@NotNull InterceptorRegistry registry) {
        if (rateLimitInterceptor != null) {
            registry.addInterceptor(rateLimitInterceptor)
                    .addPathPatterns("/api/**")
                    .excludePathPatterns(
                        "/api/v1/health/**",
                        "/api/v1/webhooks/**"
                    );
        }
    }
}
