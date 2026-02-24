package ai.aipr.server.config;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * Unit tests for Environment configuration reader.
 */
class EnvironmentTest {

    private static final String TEST_XML_FILE = "test-config.xml";

    @TempDir
    Path tempDir;

    private Path configDir;

    @BeforeEach
    void setUp() throws IOException {
        configDir = tempDir.resolve("config");
        Files.createDirectories(configDir);

        // Set RT_HOME to temp directory for tests
        System.setProperty("rt.home", tempDir.toString());
        Environment.reload();
    }

    @AfterEach
    void tearDown() {
        System.clearProperty("rt.home");
        Environment.reload();
    }

    @Nested
    @DisplayName("ConfigReader Tests")
    class ConfigReaderTests {

        @Test
        @DisplayName("should read string value from XML")
        void shouldReadStringValue() throws IOException {
            String xml = """
                <?xml version="1.0" encoding="UTF-8"?>
                <configuration>
                    <database>
                        <host>localhost</host>
                        <port>5432</port>
                        <name>testdb</name>
                    </database>
                </configuration>
                """;

            Path xmlFile = configDir.resolve(TEST_XML_FILE);
            Files.writeString(xmlFile, xml);

            Environment.ConfigReader reader = new Environment.ConfigReader(xmlFile.toFile());

            Optional<String> host = reader.getOptional("database.host");
            assertTrue(host.isPresent());
            assertEquals("localhost", host.get());

            Optional<String> name = reader.getOptional("database.name");
            assertTrue(name.isPresent());
            assertEquals("testdb", name.get());
        }

        @Test
        @DisplayName("should read integer value from XML")
        void shouldReadIntegerValue() throws IOException {
            String xml = """
                <?xml version="1.0" encoding="UTF-8"?>
                <configuration>
                    <server>
                        <port>8080</port>
                        <maxConnections>100</maxConnections>
                    </server>
                </configuration>
                """;

            Path xmlFile = configDir.resolve(TEST_XML_FILE);
            Files.writeString(xmlFile, xml);

            Environment.ConfigReader reader = new Environment.ConfigReader(xmlFile.toFile());

            int port = reader.getInt("server.port", 0);
            assertEquals(8080, port);

            int maxConnections = reader.getInt("server.maxConnections", 0);
            assertEquals(100, maxConnections);
        }

        @Test
        @DisplayName("should return default value for missing integer")
        void shouldReturnDefaultForMissingInt() throws IOException {
            String xml = """
                <?xml version="1.0" encoding="UTF-8"?>
                <configuration>
                    <server>
                        <port>8080</port>
                    </server>
                </configuration>
                """;

            Path xmlFile = configDir.resolve(TEST_XML_FILE);
            Files.writeString(xmlFile, xml);

            Environment.ConfigReader reader = new Environment.ConfigReader(xmlFile.toFile());

            int timeout = reader.getInt("server.timeout", 30000);
            assertEquals(30000, timeout);
        }

        @Test
        @DisplayName("should read boolean value from XML")
        void shouldReadBooleanValue() throws IOException {
            String xml = """
                <?xml version="1.0" encoding="UTF-8"?>
                <configuration>
                    <features>
                        <enabled>true</enabled>
                        <debug>false</debug>
                    </features>
                </configuration>
                """;

            Path xmlFile = configDir.resolve(TEST_XML_FILE);
            Files.writeString(xmlFile, xml);

            Environment.ConfigReader reader = new Environment.ConfigReader(xmlFile.toFile());

            boolean enabled = reader.getBoolean("features.enabled", false);
            assertTrue(enabled);

            boolean debug = reader.getBoolean("features.debug", true);
            assertFalse(debug);
        }

        @Test
        @DisplayName("should read list values from XML")
        void shouldReadListValues() throws IOException {
            String xml = """
                <?xml version="1.0" encoding="UTF-8"?>
                <configuration>
                    <hosts>
                        <host>server1.example.com</host>
                        <host>server2.example.com</host>
                        <host>server3.example.com</host>
                    </hosts>
                </configuration>
                """;

            Path xmlFile = configDir.resolve(TEST_XML_FILE);
            Files.writeString(xmlFile, xml);

            Environment.ConfigReader reader = new Environment.ConfigReader(xmlFile.toFile());

            // Note: Current ConfigReader implementation doesn't support lists.
            // This test verifies the last value is stored (due to same key)
            String host = reader.get("hosts.host");
            assertNotNull(host);
            // The last "host" element overwrites previous ones in current implementation
            assertEquals("server3.example.com", host);
        }

        @Test
        @DisplayName("should return empty optional for missing path")
        void shouldReturnEmptyForMissingPath() throws IOException {
            String xml = """
                <?xml version="1.0" encoding="UTF-8"?>
                <configuration>
                    <server>
                        <port>8080</port>
                    </server>
                </configuration>
                """;

            Path xmlFile = configDir.resolve(TEST_XML_FILE);
            Files.writeString(xmlFile, xml);

            Environment.ConfigReader reader = new Environment.ConfigReader(xmlFile.toFile());

            Optional<String> missing = reader.getOptional("database.host");
            assertFalse(missing.isPresent());
        }

        @Test
        @DisplayName("should handle nested paths correctly")
        void shouldHandleNestedPaths() throws IOException {
            String xml = """
                <?xml version="1.0" encoding="UTF-8"?>
                <configuration>
                    <database>
                        <pool>
                            <maxSize>20</maxSize>
                            <minIdle>5</minIdle>
                        </pool>
                    </database>
                </configuration>
                """;

            Path xmlFile = configDir.resolve(TEST_XML_FILE);
            Files.writeString(xmlFile, xml);

            Environment.ConfigReader reader = new Environment.ConfigReader(xmlFile.toFile());

            int maxSize = reader.getInt("database.pool.maxSize", 0);
            assertEquals(20, maxSize);

            int minIdle = reader.getInt("database.pool.minIdle", 0);
            assertEquals(5, minIdle);
        }
    }

    @Nested
    @DisplayName("Path Resolution Tests")
    class PathResolutionTests {

        @BeforeEach
        void setUpPathTests() {
            // Ensure the environment is reset with THIS test's tempDir
            System.setProperty("rt.home", tempDir.toString());
            Environment.reload();
        }

        @Test
        @DisplayName("should resolve config directory")
        void shouldResolveConfigDir() {
            Path configPath = Environment.getConfigDir();
            assertNotNull(configPath);
            assertEquals(tempDir.resolve("config"), configPath);
        }

        @Test
        @DisplayName("should resolve data directory")
        void shouldResolveDataDir() {
            Path dataPath = Environment.getDataDir();
            assertNotNull(dataPath);
            assertEquals(tempDir.resolve("data"), dataPath);
        }

        @Test
        @DisplayName("should resolve lib directory")
        void shouldResolveLibDir() {
            Path libPath = Environment.getLibDir();
            assertNotNull(libPath);
            assertEquals(tempDir.resolve("lib"), libPath);
        }
    }
}
