package ai.aipr.server.config;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.w3c.dom.Document;
import org.w3c.dom.Element;
import org.w3c.dom.Node;
import org.w3c.dom.NodeList;

import javax.xml.parsers.DocumentBuilder;
import javax.xml.parsers.DocumentBuilderFactory;
import java.io.File;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.HashMap;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Environment utility for reading XML-based configuration files.
 */
public final class Environment {
    
    private static final Logger log = LoggerFactory.getLogger(Environment.class);
    
    private static final String RT_HOME_ENV = "RT_HOME";
    private static final String CONFIG_DIR = "config";
    private static final String SERVER_CONFIG_FILE = "rtserverprops.xml";
    private static final String PLATFORMS_CONFIG_FILE = "vcsplatforms.xml";
    
    private static final AtomicReference<ConfigReader> serverConfig = new AtomicReference<>();
    private static final AtomicReference<ConfigReader> platformsConfig = new AtomicReference<>();
    private static final AtomicReference<String> rtHomeRef = new AtomicReference<>();
    
    private Environment() {
        // Utility class
    }
    
    /**
     * Gets the RT_HOME directory path.
     * 
     * @return the RT_HOME path
     * @throws IllegalStateException if RT_HOME is not set
     */
    public static String getRtHome() {
        String cached = rtHomeRef.get();
        if (cached != null) {
            return cached;
        }
        
        synchronized (Environment.class) {
            cached = rtHomeRef.get();
            if (cached != null) {
                return cached;
            }
            
            String home = System.getenv(RT_HOME_ENV);
            if (home == null || home.isBlank()) {
                // Try system property
                home = System.getProperty("rt.home");
            }
            if (home == null || home.isBlank()) {
                // Default to current directory
                home = System.getProperty("user.dir");
                log.warn("RT_HOME not set, using current directory: {}", home);
            }
            log.info("RT_HOME: {}", home);
            rtHomeRef.set(home);
            return home;
        }
    }
    
    /**
     * Gets the config directory path.
     * 
     * @return the config directory path
     */
    public static Path getConfigDir() {
        return Paths.get(getRtHome(), CONFIG_DIR);
    }
    
    /**
     * Gets the data directory path.
     * 
     * @return the data directory path
     */
    public static Path getDataDir() {
        return Paths.get(getRtHome(), "data");
    }
    
    /**
     * Gets the lib directory path.
     * 
     * @return the lib directory path
     */
    public static Path getLibDir() {
        return Paths.get(getRtHome(), "lib");
    }
    
    /**
     * Gets the server configuration reader.
     * 
     * @return the server config reader
     */
    public static ConfigReader server() {
        ConfigReader cached = serverConfig.get();
        if (cached != null) {
            return cached;
        }
        
        Path configPath = getConfigDir().resolve(SERVER_CONFIG_FILE);
        ConfigReader reader = new ConfigReader(configPath.toFile());
        serverConfig.compareAndSet(null, reader);
        return serverConfig.get();
    }
    
    /**
     * Gets the platforms configuration reader.
     * 
     * @return the platforms config reader
     */
    public static ConfigReader platforms() {
        ConfigReader cached = platformsConfig.get();
        if (cached != null) {
            return cached;
        }
        
        Path configPath = getConfigDir().resolve(PLATFORMS_CONFIG_FILE);
        ConfigReader reader = new ConfigReader(configPath.toFile());
        platformsConfig.compareAndSet(null, reader);
        return platformsConfig.get();
    }
    
    /**
     * Reloads all configuration files.
     */
    public static void reload() {
        serverConfig.set(null);
        platformsConfig.set(null);
        log.info("Configuration reloaded");
    }
    
    /**
     * Resolves variables in a string (e.g., ${RT_HOME}).
     * 
     * @param value the value to resolve
     * @return the resolved value
     */
    public static String resolveVariables(String value) {
        if (value == null) {
            return null;
        }
        return value
            .replace("${RT_HOME}", getRtHome())
            .replace("${rt.home}", getRtHome());
    }
    
    /**
     * Configuration reader for XML files.
     */
    public static class ConfigReader {
        
        private final File configFile;
        private final Map<String, String> cache = new ConcurrentHashMap<>();
        
        ConfigReader(File configFile) {
            this.configFile = configFile;
            load();
        }
        
        private void load() {
            if (!configFile.exists()) {
                log.warn("Configuration file not found: {}", configFile.getAbsolutePath());
                return;
            }
            
            try {
                DocumentBuilderFactory factory = DocumentBuilderFactory.newInstance();
                // Security: Disable external entities
                factory.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true);
                factory.setFeature("http://xml.org/sax/features/external-general-entities", false);
                factory.setFeature("http://xml.org/sax/features/external-parameter-entities", false);
                
                DocumentBuilder builder = factory.newDocumentBuilder();
                Document document = builder.parse(configFile);
                document.getDocumentElement().normalize();
                
                // Parse all values into cache
                parseElement(document.getDocumentElement(), "");
                
                log.info("Loaded configuration from: {}", configFile.getAbsolutePath());
            } catch (Exception e) {
                log.error("Failed to load configuration: {}", configFile.getAbsolutePath(), e);
            }
        }
        
        private void parseElement(Element element, String prefix) {
            NodeList children = element.getChildNodes();
            for (int i = 0; i < children.getLength(); i++) {
                Node node = children.item(i);
                if (node.getNodeType() == Node.ELEMENT_NODE) {
                    Element child = (Element) node;
                    String key = prefix.isEmpty() ? child.getTagName() : prefix + "." + child.getTagName();
                    
                    // Check if this element has only text content
                    if (child.getChildNodes().getLength() == 1 && 
                        child.getFirstChild().getNodeType() == Node.TEXT_NODE) {
                        String value = child.getTextContent().trim();
                        value = resolveVariables(value);
                        cache.put(key, value);
                    } else {
                        // Recurse into child elements
                        parseElement(child, key);
                    }
                }
            }
        }
        
        /**
         * Gets a string value.
         * 
         * @param key the property key (e.g., "database.host")
         * @return the value or null if not found
         */
        public String get(String key) {
            return cache.get(key);
        }
        
        /**
         * Gets a string value with default.
         * 
         * @param key the property key
         * @param defaultValue the default value
         * @return the value or default if not found
         */
        public String get(String key, String defaultValue) {
            return cache.getOrDefault(key, defaultValue);
        }
        
        /**
         * Gets an Optional string value.
         * 
         * @param key the property key
         * @return Optional containing the value
         */
        public Optional<String> getOptional(String key) {
            return Optional.ofNullable(cache.get(key)).filter(s -> !s.isBlank());
        }
        
        /**
         * Gets an integer value.
         * 
         * @param key the property key
         * @param defaultValue the default value
         * @return the integer value or default
         */
        public int getInt(String key, int defaultValue) {
            String value = cache.get(key);
            if (value == null || value.isBlank()) {
                return defaultValue;
            }
            try {
                return Integer.parseInt(value);
            } catch (NumberFormatException e) {
                log.warn("Invalid integer value for {}: {}", key, value);
                return defaultValue;
            }
        }
        
        /**
         * Gets a long value.
         * 
         * @param key the property key
         * @param defaultValue the default value
         * @return the long value or default
         */
        public long getLong(String key, long defaultValue) {
            String value = cache.get(key);
            if (value == null || value.isBlank()) {
                return defaultValue;
            }
            try {
                return Long.parseLong(value);
            } catch (NumberFormatException e) {
                log.warn("Invalid long value for {}: {}", key, value);
                return defaultValue;
            }
        }
        
        /**
         * Gets a boolean value.
         * 
         * @param key the property key
         * @param defaultValue the default value
         * @return the boolean value or default
         */
        public boolean getBoolean(String key, boolean defaultValue) {
            String value = cache.get(key);
            if (value == null || value.isBlank()) {
                return defaultValue;
            }
            return "true".equalsIgnoreCase(value) || "yes".equalsIgnoreCase(value) || "1".equals(value);
        }
        
        /**
         * Gets all properties with a given prefix.
         * 
         * @param prefix the key prefix
         * @return map of matching properties
         */
        public Map<String, String> getWithPrefix(String prefix) {
            Map<String, String> result = new HashMap<>();
            String searchPrefix = prefix.endsWith(".") ? prefix : prefix + ".";
            for (Map.Entry<String, String> entry : cache.entrySet()) {
                if (entry.getKey().startsWith(searchPrefix)) {
                    result.put(entry.getKey(), entry.getValue());
                }
            }
            return result;
        }
        
        /**
         * Checks if a key exists and has a non-empty value.
         * 
         * @param key the property key
         * @return true if the key exists with a non-empty value
         */
        public boolean has(String key) {
            String value = cache.get(key);
            return value != null && !value.isBlank();
        }
        
        /**
         * Gets all cached properties.
         * 
         * @return unmodifiable map of all properties
         */
        public Map<String, String> getAll() {
            return Map.copyOf(cache);
        }
        
        /**
         * Resolves variables in a value.
         */
        private String resolveVariables(String value) {
            return Environment.resolveVariables(value);
        }
    }
}
