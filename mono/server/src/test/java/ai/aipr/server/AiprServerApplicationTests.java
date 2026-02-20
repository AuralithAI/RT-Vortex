package ai.aipr.server;

import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;

/**
 * Integration test to verify the Spring Boot application context loads correctly.
 */
@SpringBootTest
@ActiveProfiles("test")
class AiprServerApplicationTests {

    @Test
    void contextLoads() {
        // Verifies that the Spring application context starts without errors
    }
}
