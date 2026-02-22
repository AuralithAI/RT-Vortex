package ai.aipr.server.config;

import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.w3c.dom.Document;
import org.w3c.dom.Element;
import org.w3c.dom.NamedNodeMap;
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
 * Reads XML configuration files ({@code rtserverprops.xml}, {@code vcsplatforms.xml})
 * and provides a flattened key/value {@link ConfigReader} for each.
 *
 * <h3>XML format</h3>
 * <p>Elements use <b>attributes</b> for values (Capital Essentials style):</p>
 * <pre>{@code
 * <database url="jdbc:..." username="aipr" password="secret">
 *     <pool max-size="20" min-idle="5"/>
 * </database>
 * }</pre>
 * <p>This produces keys: {@code database.url}, {@code database.username},
 * {@code database.pool.max-size}, etc.</p>
 *
 * <p>Text-node children are also supported for backward compatibility:</p>
 * <pre>{@code <host>localhost</host>  →  key "host" = "localhost"}</pre>
 *
 * <h3>Variable resolution</h3>
 * <p>Values containing {@code ${ENV_VAR:default}} are resolved at parse time:
 * environment variable first, then the default after the colon.</p>
 *
 * <h3>RT_HOME resolution</h3>
 * <ol>
 *   <li>{@code RT_HOME} environment variable</li>
 *   <li>{@code rt.home} system property</li>
 *   <li>Current working directory (dev fallback)</li>
 * </ol>
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

    private Environment() {}

    // =========================================================================
    // RT_HOME
    // =========================================================================

    public static String getRtHome() {
        String cached = rtHomeRef.get();
        if (cached != null) return cached;

        synchronized (Environment.class) {
            cached = rtHomeRef.get();
            if (cached != null) return cached;

            String home = System.getenv(RT_HOME_ENV);
            if (isBlank(home)) home = System.getProperty("rt.home");
            if (isBlank(home)) {
                home = System.getProperty("user.dir");
                log.warn("RT_HOME not set, using current directory: {}", home);
            }
            log.info("RT_HOME: {}", home);
            rtHomeRef.set(home);
            return home;
        }
    }

    // =========================================================================
    // Well-known sub-paths
    // =========================================================================

    @NotNull public static Path getConfigDir()       { return Paths.get(getRtHome(), CONFIG_DIR); }
    @NotNull public static Path getDataDir()         { return Paths.get(getRtHome(), "data"); }
    @NotNull public static Path getLibDir()          { return Paths.get(getRtHome(), "lib"); }
    @NotNull public static Path getCertificatesDir() { return Paths.get(getRtHome(), "certificates"); }

    // =========================================================================
    // Config readers
    // =========================================================================

    /** Server configuration from {@code rtserverprops.xml}. */
    public static ConfigReader server() {
        ConfigReader cached = serverConfig.get();
        if (cached != null) return cached;
        ConfigReader reader = new ConfigReader(getConfigDir().resolve(SERVER_CONFIG_FILE).toFile());
        serverConfig.compareAndSet(null, reader);
        return serverConfig.get();
    }

    /** Platform configuration from {@code vcsplatforms.xml}. */
    public static ConfigReader platforms() {
        ConfigReader cached = platformsConfig.get();
        if (cached != null) return cached;
        ConfigReader reader = new ConfigReader(getConfigDir().resolve(PLATFORMS_CONFIG_FILE).toFile());
        platformsConfig.compareAndSet(null, reader);
        return platformsConfig.get();
    }

    /** Reload all configuration files. */
    public static void reload() {
        rtHomeRef.set(null);
        serverConfig.set(null);
        platformsConfig.set(null);
        log.info("Configuration reloaded");
    }

    // =========================================================================
    // Variable resolution
    // =========================================================================

    /**
     * Resolve all {@code ${VAR:default}} placeholders in a value.
     * Also replaces {@code ${RT_HOME}} and {@code ${aipr.home}}.
     */
    @NotNull
    public static String resolveVariables(String value) {
        if (value == null) return "";

        // First resolve ${RT_HOME} / ${aipr.home}
        String home = getRtHome();
        value = value.replace("${RT_HOME}", home)
                     .replace("${rt.home}", home)
                     .replace("${aipr.home}", home);

        // Then resolve ${ENV_VAR:default} patterns
        return resolveEnvPlaceholders(value);
    }

    /**
     * Iteratively resolve {@code ${ENV:default}} tokens.
     */
    @NotNull
    private static String resolveEnvPlaceholders(@NotNull String value) {
        StringBuilder result = new StringBuilder();
        int i = 0;
        while (i < value.length()) {
            int start = value.indexOf("${", i);
            if (start == -1) {
                result.append(value, i, value.length());
                break;
            }
            result.append(value, i, start);
            int end = value.indexOf('}', start + 2);
            if (end == -1) {
                result.append(value, start, value.length());
                break;
            }
            String placeholder = value.substring(start + 2, end);
            int colonIdx = placeholder.indexOf(':');
            String envKey = colonIdx >= 0 ? placeholder.substring(0, colonIdx) : placeholder;
            String defaultVal = colonIdx >= 0 ? placeholder.substring(colonIdx + 1) : "";

            String resolved = System.getenv(envKey);
            if (isBlank(resolved)) resolved = System.getProperty(envKey);
            if (isBlank(resolved)) resolved = defaultVal;
            result.append(resolved);
            i = end + 1;
        }
        return result.toString();
    }

    private static boolean isBlank(String s) { return s == null || s.isBlank(); }

    // =========================================================================
    // ConfigReader — parses XML into flattened key/value pairs
    // =========================================================================

    public static class ConfigReader {

        private final File configFile;
        private final Map<String, String> cache = new ConcurrentHashMap<>();

        ConfigReader(File configFile) {
            this.configFile = configFile;
            load();
        }

        /** Package-private constructor for testing or PropertySource bridge. */
        ConfigReader(Map<String, String> preloaded) {
            this.configFile = null;
            this.cache.putAll(preloaded);
        }

        private void load() {
            if (!configFile.exists()) {
                log.warn("Configuration file not found: {}", configFile.getAbsolutePath());
                return;
            }

            try {
                DocumentBuilderFactory factory = DocumentBuilderFactory.newInstance();
                factory.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true);
                factory.setFeature("http://xml.org/sax/features/external-general-entities", false);
                factory.setFeature("http://xml.org/sax/features/external-parameter-entities", false);

                DocumentBuilder builder = factory.newDocumentBuilder();
                Document document = builder.parse(configFile);
                document.getDocumentElement().normalize();

                parseElement(document.getDocumentElement(), "");
                log.info("Loaded {} properties from: {}", cache.size(), configFile.getName());
            } catch (Exception e) {
                log.error("Failed to load configuration: {}", configFile.getAbsolutePath(), e);
            }
        }

        /**
         * Parse an element and its children into the flat cache.
         *
         * <ul>
         *   <li><b>Attributes:</b> {@code <elem key="val"/>} → {@code prefix.key = val}</li>
         *   <li><b>Text children:</b> {@code <elem>val</elem>} → {@code prefix = val}</li>
         *   <li><b>Nested elements:</b> recurse with extended prefix</li>
         * </ul>
         */
        private void parseElement(@NotNull Element element, String prefix) {
            // 1) Read attributes on this element
            NamedNodeMap attrs = element.getAttributes();
            for (int i = 0; i < attrs.getLength(); i++) {
                Node attr = attrs.item(i);
                String attrName = attr.getNodeName();
                // Skip xmlns and xml:* attributes
                if (attrName.startsWith("xmlns") || attrName.startsWith("xml:")) continue;
                String key = prefix.isEmpty() ? attrName : prefix + "." + attrName;
                cache.put(key, resolveVariables(attr.getNodeValue().trim()));
            }

            // 2) Process child nodes
            NodeList children = element.getChildNodes();
            for (int i = 0; i < children.getLength(); i++) {
                Node node = children.item(i);
                if (node.getNodeType() == Node.ELEMENT_NODE) {
                    Element child = (Element) node;
                    String childKey = prefix.isEmpty()
                            ? child.getTagName()
                            : prefix + "." + child.getTagName();

                    // If this element has only a text node child → leaf value
                    if (child.getChildNodes().getLength() == 1
                            && child.getFirstChild().getNodeType() == Node.TEXT_NODE) {
                        String text = child.getTextContent().trim();
                        if (!text.isEmpty()) {
                            cache.put(childKey, resolveVariables(text));
                        }
                        // Also parse any attributes on this text-bearing element
                        parseAttributes(child, childKey);
                    } else {
                        // Recurse into child elements
                        parseElement(child, childKey);
                    }
                }
            }
        }

        /** Parse only the attributes of an element (helper for text-bearing elements). */
        private void parseAttributes(@NotNull Element element, String prefix) {
            NamedNodeMap attrs = element.getAttributes();
            for (int i = 0; i < attrs.getLength(); i++) {
                Node attr = attrs.item(i);
                String attrName = attr.getNodeName();
                if (attrName.startsWith("xmlns") || attrName.startsWith("xml:")) continue;
                String key = prefix + "." + attrName;
                cache.put(key, resolveVariables(attr.getNodeValue().trim()));
            }
        }

        // =====================================================================
        // Public accessors (unchanged API)
        // =====================================================================

        public String get(String key) { return cache.get(key); }

        public String get(String key, String defaultValue) {
            return cache.getOrDefault(key, defaultValue);
        }

        public Optional<String> getOptional(String key) {
            return Optional.ofNullable(cache.get(key)).filter(s -> !s.isBlank());
        }

        public int getInt(String key, int defaultValue) {
            String v = cache.get(key);
            if (v == null || v.isBlank()) return defaultValue;
            try { return Integer.parseInt(v.trim()); }
            catch (NumberFormatException e) { log.warn("Invalid int for {}: {}", key, v); return defaultValue; }
        }

        public long getLong(String key, long defaultValue) {
            String v = cache.get(key);
            if (v == null || v.isBlank()) return defaultValue;
            try { return Long.parseLong(v.trim()); }
            catch (NumberFormatException e) { log.warn("Invalid long for {}: {}", key, v); return defaultValue; }
        }

        public boolean getBoolean(String key, boolean defaultValue) {
            String v = cache.get(key);
            if (v == null || v.isBlank()) return defaultValue;
            return "true".equalsIgnoreCase(v.trim()) || "yes".equalsIgnoreCase(v.trim()) || "1".equals(v.trim());
        }

        public Map<String, String> getWithPrefix(@NotNull String prefix) {
            String search = prefix.endsWith(".") ? prefix : prefix + ".";
            Map<String, String> result = new HashMap<>();
            for (Map.Entry<String, String> entry : cache.entrySet()) {
                if (entry.getKey().startsWith(search)) {
                    result.put(entry.getKey(), entry.getValue());
                }
            }
            return result;
        }

        public boolean has(String key) {
            String v = cache.get(key);
            return v != null && !v.isBlank();
        }

        public Map<String, String> getAll() { return Map.copyOf(cache); }
    }
}
