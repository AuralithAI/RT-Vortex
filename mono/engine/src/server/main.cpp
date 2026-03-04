/**
 * RTVortex - Code Intelligence & Review Engine
 *
 * gRPC server entry point for the C++ semantic engine.
 * Handles:
 *   - CLI argument parsing (--host, --port, --config)
 *   - YAML config loading
 *   - TLS credential loading
 *   - Graceful shutdown on SIGTERM/SIGINT
 *   - Windows service control (install/uninstall/start/stop/run)
 *
 * Windows Service Commands:
 *   rtvortex.exe install   - Install as Windows service
 *   rtvortex.exe uninstall - Remove Windows service
 *   rtvortex.exe start     - Start the service
 *   rtvortex.exe stop      - Stop the service
 *   rtvortex.exe run       - Run in foreground (default)
 */

#include "engine_service_impl.h"
#include "engine_api.h"
#include "version.h"
#include "splash_screen.h"

#include <grpcpp/grpcpp.h>
#include <grpcpp/health_check_service_interface.h>
#include <grpcpp/ext/proto_server_reflection_plugin.h>

#include <iostream>
#include <fstream>
#include <sstream>
#include <string>
#include <memory>
#include <atomic>
#include <csignal>
#include <thread>
#include <chrono>
#include <ctime>
#include <iomanip>
#include <filesystem>

#ifdef _WIN32
#include <windows.h>
#include <winsvc.h>
#include <tchar.h>
#else
#include <unistd.h>
#endif

namespace fs = std::filesystem;

// Service constants
#ifdef _WIN32
#define SERVICE_NAME        TEXT("RTVortex")
#define SERVICE_DISPLAY     TEXT("RTVortex - Code Intelligence & Review Engine")
#define SERVICE_DESCRIPTION TEXT("High-performance code indexing, semantic retrieval, and review engine")
#endif

namespace {

//=============================================================================
// RTVortex Environment 
//=============================================================================

struct RtVortexEnvironment {
    std::string home;        // RTVORTEX_HOME — root directory
    std::string config_dir;  // RTVORTEX_HOME/config
    std::string data_dir;    // RTVORTEX_HOME/data
    std::string temp_dir;    // RTVORTEX_HOME/temp
    std::string models_dir;  // RTVORTEX_HOME/models
    std::string hostname;    // machine hostname
};

static RtVortexEnvironment g_env;
static std::ofstream g_log_file;

/**
 * Resolve RTVORTEX_HOME:
 *   1. RTVORTEX_HOME env var
 *   2. AIPR_HOME env var (backward compat)
 *   3. RT_HOME env var (shared with Java server)
 *   4. Executable's parent directory
 *   5. Current working directory (dev fallback)
 */
std::string resolveHome() {
    if (const char* v = std::getenv("RTVORTEX_HOME")) return v;
    if (const char* v = std::getenv("AIPR_HOME"))     return v;  // backward compat
    if (const char* v = std::getenv("RT_HOME"))       return v;

    // Try executable's parent dir (e.g., /opt/rtvortex/bin/rtvortex → /opt/rtvortex)
    try {
#ifdef _WIN32
        char buf[MAX_PATH];
        GetModuleFileNameA(NULL, buf, MAX_PATH);
        fs::path exe_dir = fs::path(buf).parent_path();
#else
        fs::path exe_dir = fs::canonical("/proc/self/exe").parent_path();
#endif
        // If binary is in bin/, go up one level
        if (exe_dir.filename() == "bin") {
            return exe_dir.parent_path().string();
        }
        return exe_dir.string();
    } catch (...) {}

    return fs::current_path().string();
}

std::string getHostname() {
#ifdef _WIN32
    char buf[256];
    DWORD size = sizeof(buf);
    if (GetComputerNameA(buf, &size)) return buf;
    return "unknown";
#else
    char buf[256];
    if (gethostname(buf, sizeof(buf)) == 0) return buf;
    return "unknown";
#endif
}

std::string currentTimestampForLog() {
    auto now = std::chrono::system_clock::now();
    auto time = std::chrono::system_clock::to_time_t(now);
    auto ms = std::chrono::duration_cast<std::chrono::milliseconds>(
        now.time_since_epoch()) % 1000;
    std::ostringstream ss;
    ss << std::put_time(std::gmtime(&time), "%Y-%m-%d %H:%M:%S")
       << '.' << std::setfill('0') << std::setw(3) << ms.count();
    return ss.str();
}

std::string currentDateStamp() {
    auto now = std::chrono::system_clock::now();
    auto time = std::chrono::system_clock::to_time_t(now);
    char buf[16];
    std::strftime(buf, sizeof(buf), "%Y-%m-%d", std::gmtime(&time));
    return buf;
}

/**
 * Initialize the RTVortex environment: resolve directories, create them,
 * and open a timestamped log file.
 */
void initEnvironment() {
    g_env.home       = resolveHome();
    g_env.config_dir = (fs::path(g_env.home) / "config").string();
    g_env.data_dir   = (fs::path(g_env.home) / "data").string();
    g_env.temp_dir   = (fs::path(g_env.home) / "temp").string();
    g_env.models_dir = (fs::path(g_env.home) / "models").string();
    g_env.hostname   = getHostname();

    // Create directories if they don't exist
    for (const auto& dir : {g_env.data_dir, g_env.temp_dir}) {
        std::error_code ec;
        fs::create_directories(dir, ec);
    }

    // Open log file: temp/rtvortex_<hostname>_<date>.log
    std::string log_filename = "rtvortex_" + g_env.hostname + "_" + currentDateStamp() + ".log";
    std::string log_path = (fs::path(g_env.temp_dir) / log_filename).string();

    g_log_file.open(log_path, std::ios::app);
    if (!g_log_file.is_open()) {
        std::cerr << "[WARN] Cannot open log file: " << log_path << "\n";
    }
}

enum class LogLevel { DEBUG, INFO, WARN, ERROR, FATAL };

void logMessage(LogLevel level, const std::string& msg) {
    static const char* labels[] = {"DEBUG", "INFO ", "WARN ", "ERROR", "FATAL"};
    int idx = static_cast<int>(level);

    std::ostringstream line;
    line << currentTimestampForLog()
         << " [" << labels[idx] << "] "
         << "[" << g_env.hostname << "] "
         << msg;

    std::string out = line.str();

    // Always write to console
    if (level >= LogLevel::ERROR) {
        std::cerr << out << "\n";
    } else {
        std::cout << out << "\n";
    }

    // Write to log file
    if (g_log_file.is_open()) {
        g_log_file << out << "\n";
        g_log_file.flush();
    }
}

// Convenience macros
#define LOG_DEBUG(msg) logMessage(LogLevel::DEBUG, msg)
#define LOG_INFO(msg)  logMessage(LogLevel::INFO,  msg)
#define LOG_WARN(msg)  logMessage(LogLevel::WARN,  msg)
#define LOG_ERROR(msg) logMessage(LogLevel::ERROR, msg)
#define LOG_FATAL(msg) logMessage(LogLevel::FATAL, msg)

//=============================================================================
// Global State for Signal Handling
//=============================================================================

std::atomic<bool> g_shutdown_requested{false};
std::unique_ptr<grpc::Server> g_server;

//=============================================================================
// CLI Argument Parsing
//=============================================================================

struct ServerConfig {
    std::string host = "0.0.0.0";
    uint16_t port = 50051;
    std::string config_path = "config/default.yml";
    
    // TLS settings (loaded from config file)
    bool tls_enabled = false;
    std::string tls_cert_chain;
    std::string tls_private_key;
    std::string tls_root_certs;  // For client auth (mTLS)
    bool tls_require_client_auth = false;
    
    // Run mode
    enum class Mode {
        Run,           // Foreground console mode (default)
        Service,       // Windows service mode
        Install,       // Install Windows service
        Uninstall,     // Uninstall Windows service  
        Start,         // Start Windows service
        Stop           // Stop Windows service
    };
    Mode mode = Mode::Run;
    
    // Debug/verbose mode
    bool verbose = false;
    
    // Splash screen (Windows/macOS only, skipped on Linux)
    bool no_splash = false;
};

void printUsage(const char* program_name) {
    std::cout << "RTVortex - Code Intelligence & Review Engine\n"
              << "\n"
              << "Usage: " << program_name << " [COMMAND] [OPTIONS]\n"
              << "\n"
#ifdef _WIN32
              << "Commands (Windows):\n"
              << "  install             Install as Windows service\n"
              << "  uninstall           Remove Windows service\n"
              << "  start               Start the Windows service\n"
              << "  stop                Stop the Windows service\n"
              << "  run                 Run in foreground (default)\n"
              << "\n"
#endif
              << "Options:\n"
              << "  --host <address>    Bind address (default: 0.0.0.0)\n"
              << "  --port <port>       Bind port (default: 50051)\n"
              << "  --config <path>     Config file path (default: config/default.yml)\n"
              << "  --verbose           Enable verbose logging\n"
              << "  -noSplashScreen     Disable the GUI splash screen\n"
              << "  --version           Print version and exit\n"
              << "  --help              Print this help and exit\n"
              << "\n"
              << "Environment variables:\n"
              << "  RTVORTEX_HOME       Engine root directory (auto-detected if not set)\n"
              << "  ENGINE_HOST         Override --host\n"
              << "  ENGINE_PORT         Override --port\n"
              << "  ENGINE_CONFIG       Override --config\n"
              << "  ENGINE_TLS_ENABLED  Enable TLS (true/false)\n"
              << "  ENGINE_TLS_CERT     Path to server certificate\n"
              << "  ENGINE_TLS_KEY      Path to server private key\n"
              << "  ENGINE_TLS_CA       Path to CA certificate (for mTLS)\n"
              << "  ENGINE_TLS_CLIENT_AUTH  Require client certificate (true/false)\n"
              << "\n"
              << "Directory layout (relative to RTVORTEX_HOME):\n"
              << "  config/             Configuration files\n"
              << "  data/               Persistent data (indexes, caches)\n"
              << "  temp/               Logs + temporary working files\n"
              << "  models/             ONNX embedding models\n"
              << "\n"
#ifdef _WIN32
              << "Examples:\n"
              << "  " << program_name << " install         Install service\n"
              << "  " << program_name << " start           Start service\n"
              << "  " << program_name << " run --port 50052  Run on custom port\n"
              << "\n"
#endif
              ;
}

ServerConfig parseArgs(int argc, char* argv[]) {
    ServerConfig config;
    
    // First, apply environment variable overrides
    if (const char* env = std::getenv("ENGINE_HOST")) {
        config.host = env;
    }
    if (const char* env = std::getenv("ENGINE_PORT")) {
        config.port = static_cast<uint16_t>(std::stoi(env));
    }
    if (const char* env = std::getenv("ENGINE_CONFIG")) {
        config.config_path = env;
    }
    if (const char* env = std::getenv("ENGINE_TLS_ENABLED")) {
        config.tls_enabled = (std::string(env) == "true" || std::string(env) == "1");
    }
    if (const char* env = std::getenv("ENGINE_TLS_CERT")) {
        config.tls_cert_chain = env;
    }
    if (const char* env = std::getenv("ENGINE_TLS_KEY")) {
        config.tls_private_key = env;
    }
    if (const char* env = std::getenv("ENGINE_TLS_CA")) {
        config.tls_root_certs = env;
    }
    if (const char* env = std::getenv("ENGINE_TLS_CLIENT_AUTH")) {
        config.tls_require_client_auth = (std::string(env) == "true" || std::string(env) == "1");
    }
    
    // Parse command line arguments (override env vars)
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        
        if (arg == "--help" || arg == "-h") {
            printUsage(argv[0]);
            std::exit(0);
        }
        else if (arg == "--version" || arg == "-v") {
            std::cout << "rtvortex " << AIPR_VERSION_FULL << "\n";
            std::exit(0);
        }
        else if (arg == "--verbose") {
            config.verbose = true;
        }
        else if (arg == "-noSplashScreen") {
            config.no_splash = true;
        }
#ifdef _WIN32
        // Windows service commands
        else if (arg == "install") {
            config.mode = ServerConfig::Mode::Install;
        }
        else if (arg == "uninstall") {
            config.mode = ServerConfig::Mode::Uninstall;
        }
        else if (arg == "start") {
            config.mode = ServerConfig::Mode::Start;
        }
        else if (arg == "stop") {
            config.mode = ServerConfig::Mode::Stop;
        }
        else if (arg == "run") {
            config.mode = ServerConfig::Mode::Run;
        }
        else if (arg == "--service") {
            config.mode = ServerConfig::Mode::Service;
        }
#endif
        else if (arg == "--host" && i + 1 < argc) {
            config.host = argv[++i];
        }
        else if (arg == "--port" && i + 1 < argc) {
            config.port = static_cast<uint16_t>(std::stoi(argv[++i]));
        }
        else if (arg == "--config" && i + 1 < argc) {
            config.config_path = argv[++i];
        }
        else {
            std::cerr << "Unknown argument: " << arg << "\n";
            printUsage(argv[0]);
            std::exit(1);
        }
    }
    
    return config;
}

//=============================================================================
// File Utilities
//=============================================================================

std::string readFile(const std::string& path) {
    std::ifstream file(path, std::ios::binary);
    if (!file) {
        throw std::runtime_error("Cannot open file: " + path);
    }
    std::stringstream buffer;
    buffer << file.rdbuf();
    return buffer.str();
}

bool fileExists(const std::string& path) {
    std::ifstream file(path);
    return file.good();
}

//=============================================================================
// TLS Credential Setup
//=============================================================================

std::shared_ptr<grpc::ServerCredentials> buildCredentials(const ServerConfig& config) {
    if (!config.tls_enabled) {
        LOG_INFO("TLS disabled, using insecure credentials");
        return grpc::InsecureServerCredentials();
    }
    
    LOG_INFO("TLS enabled, loading certificates...");
    
    // Read certificate and key files
    if (config.tls_cert_chain.empty() || config.tls_private_key.empty()) {
        throw std::runtime_error("TLS enabled but certificate or key path not specified");
    }
    
    if (!fileExists(config.tls_cert_chain)) {
        throw std::runtime_error("Certificate file not found: " + config.tls_cert_chain);
    }
    if (!fileExists(config.tls_private_key)) {
        throw std::runtime_error("Private key file not found: " + config.tls_private_key);
    }
    
    std::string cert_chain = readFile(config.tls_cert_chain);
    std::string private_key = readFile(config.tls_private_key);
    
    grpc::SslServerCredentialsOptions::PemKeyCertPair key_cert_pair;
    key_cert_pair.cert_chain = cert_chain;
    key_cert_pair.private_key = private_key;
    
    grpc::SslServerCredentialsOptions ssl_opts;
    ssl_opts.pem_key_cert_pairs.push_back(key_cert_pair);
    
    // Load CA certs for client authentication (mTLS)
    if (!config.tls_root_certs.empty() && fileExists(config.tls_root_certs)) {
        ssl_opts.pem_root_certs = readFile(config.tls_root_certs);
        
        if (config.tls_require_client_auth) {
            ssl_opts.client_certificate_request = 
                GRPC_SSL_REQUEST_AND_REQUIRE_CLIENT_CERTIFICATE_AND_VERIFY;
            LOG_INFO("mTLS enabled (client certificate required)");
        } else {
            ssl_opts.client_certificate_request = 
                GRPC_SSL_REQUEST_CLIENT_CERTIFICATE_BUT_DONT_VERIFY;
            LOG_INFO("TLS with optional client certificate");
        }
    } else {
        ssl_opts.client_certificate_request = GRPC_SSL_DONT_REQUEST_CLIENT_CERTIFICATE;
        LOG_INFO("TLS enabled (server-side only)");
    }
    
    return grpc::SslServerCredentials(ssl_opts);
}

//=============================================================================
// Signal Handling
//=============================================================================

void signalHandler(int signal) {
    // Signal handlers must be async-signal-safe.
    // Only set the flag here — do NOT call g_server->Shutdown() from the
    // signal handler, as it causes a mutex deadlock with server->Wait().
    g_shutdown_requested = true;
    
    // This is safe: Shutdown() from a different context unblocks Wait().
    // We use a detached thread so the signal handler returns immediately.
    std::thread([]() {
        if (g_server) {
            auto deadline = std::chrono::system_clock::now() + std::chrono::seconds(30);
            g_server->Shutdown(deadline);
        }
    }).detach();
}

void setupSignalHandlers() {
#ifdef _WIN32
    signal(SIGINT, signalHandler);
    signal(SIGTERM, signalHandler);
#else
    struct sigaction sa;
    sa.sa_handler = signalHandler;
    sigemptyset(&sa.sa_mask);
    sa.sa_flags = 0;
    
    sigaction(SIGINT, &sa, nullptr);
    sigaction(SIGTERM, &sa, nullptr);
#endif
}

//=============================================================================
// Windows Service Support
//=============================================================================

#ifdef _WIN32

SERVICE_STATUS g_service_status;
SERVICE_STATUS_HANDLE g_service_status_handle;

// Forward declarations
void runServer(const ServerConfig& config);
std::string getExecutablePath();

//-----------------------------------------------------------------------------
// Get path to current executable
//-----------------------------------------------------------------------------
std::string getExecutablePath() {
    char path[MAX_PATH];
    GetModuleFileNameA(NULL, path, MAX_PATH);
    return std::string(path);
}

//-----------------------------------------------------------------------------
// Install Windows Service
//-----------------------------------------------------------------------------
bool installService() {
    SC_HANDLE scm = OpenSCManager(NULL, NULL, SC_MANAGER_CREATE_SERVICE);
    if (!scm) {
        LOG_ERROR("Cannot open Service Control Manager. Run as Administrator.");
        return false;
    }
    
    std::string exePath = getExecutablePath();
    std::string cmdLine = "\"" + exePath + "\" --service";
    
    SC_HANDLE service = CreateServiceA(
        scm,
        "RTVortex",                            // Service name
        "RTVortex - Code Intelligence Engine", // Display name
        SERVICE_ALL_ACCESS,
        SERVICE_WIN32_OWN_PROCESS,
        SERVICE_AUTO_START,                // Start automatically
        SERVICE_ERROR_NORMAL,
        cmdLine.c_str(),
        NULL,                              // No load ordering group
        NULL,                              // No tag identifier
        NULL,                              // No dependencies
        NULL,                              // LocalSystem account
        NULL                               // No password
    );
    
    if (!service) {
        DWORD error = GetLastError();
        if (error == ERROR_SERVICE_EXISTS) {
            LOG_INFO("Service already exists.");
        } else {
            LOG_ERROR("CreateService failed: " + std::to_string(error));
        }
        CloseServiceHandle(scm);
        return error == ERROR_SERVICE_EXISTS;
    }
    
    // Set service description
    SERVICE_DESCRIPTIONA desc;
    desc.lpDescription = const_cast<LPSTR>(
        "RTVortex Code Intelligence & Review Engine. "
        "Provides gRPC API on port 50051 for the Java server."
    );
    ChangeServiceConfig2A(service, SERVICE_CONFIG_DESCRIPTION, &desc);
    
    // Configure failure actions (restart on failure)
    SC_ACTION actions[3] = {
        { SC_ACTION_RESTART, 5000 },   // Restart after 5 seconds
        { SC_ACTION_RESTART, 10000 },  // Restart after 10 seconds
        { SC_ACTION_RESTART, 30000 }   // Restart after 30 seconds
    };
    SERVICE_FAILURE_ACTIONSA sfa;
    sfa.dwResetPeriod = 86400;  // Reset failure count after 1 day
    sfa.lpRebootMsg = NULL;
    sfa.lpCommand = NULL;
    sfa.cActions = 3;
    sfa.lpsaActions = actions;
    ChangeServiceConfig2A(service, SERVICE_CONFIG_FAILURE_ACTIONS, &sfa);
    
    LOG_INFO("Service 'RTVortex' installed successfully.");
    LOG_INFO("  Start with: rtvortex start");
    LOG_INFO("  Or: sc start RTVortex");
    
    CloseServiceHandle(service);
    CloseServiceHandle(scm);
    return true;
}

//-----------------------------------------------------------------------------
// Uninstall Windows Service
//-----------------------------------------------------------------------------
bool uninstallService() {
    SC_HANDLE scm = OpenSCManager(NULL, NULL, SC_MANAGER_CONNECT);
    if (!scm) {
        LOG_ERROR("Cannot open Service Control Manager. Run as Administrator.");
        return false;
    }
    
    SC_HANDLE service = OpenServiceA(scm, "RTVortex", SERVICE_STOP | DELETE | SERVICE_QUERY_STATUS);
    if (!service) {
        DWORD error = GetLastError();
        if (error == ERROR_SERVICE_DOES_NOT_EXIST) {
            LOG_INFO("Service does not exist.");
        } else {
            LOG_ERROR("OpenService failed: " + std::to_string(error));
        }
        CloseServiceHandle(scm);
        return error == ERROR_SERVICE_DOES_NOT_EXIST;
    }
    
    // Stop service if running
    SERVICE_STATUS status;
    if (QueryServiceStatus(service, &status) && status.dwCurrentState != SERVICE_STOPPED) {
        LOG_INFO("Stopping service...");
        ControlService(service, SERVICE_CONTROL_STOP, &status);
        
        // Wait for stop
        int tries = 0;
        while (status.dwCurrentState != SERVICE_STOPPED && tries++ < 30) {
            Sleep(1000);
            QueryServiceStatus(service, &status);
        }
    }
    
    if (!DeleteService(service)) {
        LOG_ERROR("DeleteService failed: " + std::to_string(GetLastError()));
        CloseServiceHandle(service);
        CloseServiceHandle(scm);
        return false;
    }
    
    LOG_INFO("Service 'RTVortex' uninstalled successfully.");
    
    CloseServiceHandle(service);
    CloseServiceHandle(scm);
    return true;
}

//-----------------------------------------------------------------------------
// Start Windows Service
//-----------------------------------------------------------------------------
bool startService() {
    SC_HANDLE scm = OpenSCManager(NULL, NULL, SC_MANAGER_CONNECT);
    if (!scm) {
        LOG_ERROR("Cannot open Service Control Manager.");
        return false;
    }
    
    SC_HANDLE service = OpenServiceA(scm, "RTVortex", SERVICE_START | SERVICE_QUERY_STATUS);
    if (!service) {
        DWORD error = GetLastError();
        if (error == ERROR_SERVICE_DOES_NOT_EXIST) {
            LOG_ERROR("Service not installed. Run: rtvortex install");
        } else {
            LOG_ERROR("OpenService failed: " + std::to_string(error));
        }
        CloseServiceHandle(scm);
        return false;
    }
    
    SERVICE_STATUS status;
    QueryServiceStatus(service, &status);
    
    if (status.dwCurrentState == SERVICE_RUNNING) {
        LOG_INFO("Service is already running.");
        CloseServiceHandle(service);
        CloseServiceHandle(scm);
        return true;
    }
    
    if (!StartServiceA(service, 0, NULL)) {
        DWORD error = GetLastError();
        if (error == ERROR_SERVICE_ALREADY_RUNNING) {
            LOG_INFO("Service is already running.");
        } else {
            LOG_ERROR("StartService failed: " + std::to_string(error));
            CloseServiceHandle(service);
            CloseServiceHandle(scm);
            return false;
        }
    }
    
    LOG_INFO("Service 'RTVortex' started.");
    
    CloseServiceHandle(service);
    CloseServiceHandle(scm);
    return true;
}

//-----------------------------------------------------------------------------
// Stop Windows Service
//-----------------------------------------------------------------------------
bool stopService() {
    SC_HANDLE scm = OpenSCManager(NULL, NULL, SC_MANAGER_CONNECT);
    if (!scm) {
        LOG_ERROR("Cannot open Service Control Manager.");
        return false;
    }
    
    SC_HANDLE service = OpenServiceA(scm, "RTVortex", SERVICE_STOP | SERVICE_QUERY_STATUS);
    if (!service) {
        DWORD error = GetLastError();
        if (error == ERROR_SERVICE_DOES_NOT_EXIST) {
            LOG_ERROR("Service not installed.");
        } else {
            LOG_ERROR("OpenService failed: " + std::to_string(error));
        }
        CloseServiceHandle(scm);
        return false;
    }
    
    SERVICE_STATUS status;
    QueryServiceStatus(service, &status);
    
    if (status.dwCurrentState == SERVICE_STOPPED) {
        LOG_INFO("Service is already stopped.");
        CloseServiceHandle(service);
        CloseServiceHandle(scm);
        return true;
    }
    
    if (!ControlService(service, SERVICE_CONTROL_STOP, &status)) {
        LOG_ERROR("ControlService(STOP) failed: " + std::to_string(GetLastError()));
        CloseServiceHandle(service);
        CloseServiceHandle(scm);
        return false;
    }
    
    // Wait for stop
    LOG_INFO("Stopping service...");
    int tries = 0;
    while (status.dwCurrentState != SERVICE_STOPPED && tries++ < 30) {
        Sleep(1000);
        QueryServiceStatus(service, &status);
    }
    
    if (status.dwCurrentState == SERVICE_STOPPED) {
        LOG_INFO("Service 'RTVortex' stopped.");
    } else {
        LOG_WARN("Service may not have stopped cleanly.");
    }
    
    CloseServiceHandle(service);
    CloseServiceHandle(scm);
    return true;
}

//-----------------------------------------------------------------------------
// Service Control Handler
//-----------------------------------------------------------------------------
void WINAPI ServiceCtrlHandler(DWORD ctrl_code) {
    switch (ctrl_code) {
        case SERVICE_CONTROL_STOP:
        case SERVICE_CONTROL_SHUTDOWN:
            g_service_status.dwCurrentState = SERVICE_STOP_PENDING;
            g_service_status.dwWaitHint = 30000;  // 30 seconds
            SetServiceStatus(g_service_status_handle, &g_service_status);
            
            g_shutdown_requested = true;
            if (g_server) {
                auto deadline = std::chrono::system_clock::now() + std::chrono::seconds(30);
                g_server->Shutdown(deadline);
            }
            break;
            
        case SERVICE_CONTROL_INTERROGATE:
            SetServiceStatus(g_service_status_handle, &g_service_status);
            break;
            
        default:
            break;
    }
}

//-----------------------------------------------------------------------------
// Service Main Entry (called by SCM)
//-----------------------------------------------------------------------------
void WINAPI ServiceMain(DWORD argc, LPSTR* argv) {
    (void)argc;
    (void)argv;
    
    g_service_status_handle = RegisterServiceCtrlHandler(
        "RTVortex",
        ServiceCtrlHandler
    );
    
    if (!g_service_status_handle) {
        return;
    }
    
    // Initialize service status
    g_service_status.dwServiceType = SERVICE_WIN32_OWN_PROCESS;
    g_service_status.dwCurrentState = SERVICE_START_PENDING;
    g_service_status.dwControlsAccepted = SERVICE_ACCEPT_STOP | SERVICE_ACCEPT_SHUTDOWN;
    g_service_status.dwWin32ExitCode = 0;
    g_service_status.dwServiceSpecificExitCode = 0;
    g_service_status.dwCheckPoint = 0;
    g_service_status.dwWaitHint = 10000;  // 10 seconds
    
    SetServiceStatus(g_service_status_handle, &g_service_status);
    
    try {
        // Parse default config (service mode uses defaults or env vars)
        ServerConfig config;
        
        // Mark as running
        g_service_status.dwCurrentState = SERVICE_RUNNING;
        g_service_status.dwCheckPoint = 0;
        g_service_status.dwWaitHint = 0;
        SetServiceStatus(g_service_status_handle, &g_service_status);
        
        runServer(config);
        
    } catch (const std::exception& e) {
        // Log error (would use Windows Event Log in production)
        g_service_status.dwCurrentState = SERVICE_STOPPED;
        g_service_status.dwWin32ExitCode = ERROR_SERVICE_SPECIFIC_ERROR;
        g_service_status.dwServiceSpecificExitCode = 1;
        SetServiceStatus(g_service_status_handle, &g_service_status);
        return;
    }
    
    // Clean stop
    g_service_status.dwCurrentState = SERVICE_STOPPED;
    g_service_status.dwWin32ExitCode = 0;
    SetServiceStatus(g_service_status_handle, &g_service_status);
}

bool runAsService() {
    SERVICE_TABLE_ENTRY service_table[] = {
        { const_cast<LPSTR>("RTVortex"), ServiceMain },
        { nullptr, nullptr }
    };
    
    return StartServiceCtrlDispatcher(service_table) != 0;
}

#endif  // _WIN32

//=============================================================================
// Server Main Logic
//=============================================================================

void runServer(const ServerConfig& config) {
    // Build server address
    std::string server_address = config.host + ":" + std::to_string(config.port);
    
    // ── Splash Screen (Windows/macOS only) ───────────────────────────
    // On Windows/macOS: shows a native GUI splash with rv_splash.jpg
    // On Linux: headless — just prints the terminal banner below
    if (!config.no_splash) {
        std::string splash_image = (fs::path(g_env.home) / "images" / "rv_splash.jpg").string();
        rtvortex::SplashScreen::show(splash_image);
    }
    
    // ── Terminal Banner (always, all platforms) ──────────────────────
    const char* banner = R"(
    ╔═════════════════════════════════════════════════════════════╗
    ║                                                             ║
    ║   ██████╗ ████████╗                  _                      ║
    ║   ██╔══██╗╚══██╔══╝__   _____  _ __| |_ _____  __           ║
    ║   ██████╔╝   ██║   \ \ / / _ \| '__| __/ _ \ \/ /           ║
    ║   ██╔══██╗   ██║    \ V / (_) | |  | ||  __/>  <            ║
    ║   ██║  ██║   ██║     \_/ \___/|_|   \__\___/_/\_\           ║
    ║   ╚═╝  ╚═╝   ╚═╝                                            ║
    ║                                                             ║
    ║         Code Intelligence & Review Engine                   ║
    ║         Indexing · Retrieval · Semantic Analysis            ║
    ║                                                             ║
    ╚═════════════════════════════════════════════════════════════╝
)";
    std::cout << banner << std::flush;

    LOG_INFO("RTVortex starting up...");
    LOG_INFO("  Version:    " + std::string(AIPR_VERSION_FULL));
    LOG_INFO("  Build date: " + std::string(AIPR_BUILD_DATE));
    LOG_INFO("  Hostname:   " + g_env.hostname);
    LOG_INFO("  PID:        " + std::to_string(
#ifdef _WIN32
        GetCurrentProcessId()
#else
        getpid()
#endif
    ));
    LOG_INFO("  ───────────────────────────────────");
    LOG_INFO("  Home:       " + g_env.home);
    LOG_INFO("  Config dir: " + g_env.config_dir);
    LOG_INFO("  Data dir:   " + g_env.data_dir);
    LOG_INFO("  Temp dir:   " + g_env.temp_dir);
    LOG_INFO("  Models dir: " + g_env.models_dir);
    LOG_INFO("  ───────────────────────────────────");
    LOG_INFO("  Config:     " + config.config_path);
    
    // Load engine config
    aipr::EngineConfig engine_config;
    if (fileExists(config.config_path)) {
        try {
            engine_config = aipr::EngineConfig::load(config.config_path);
            LOG_INFO("Config loaded successfully");
        } catch (const std::exception& e) {
            LOG_WARN("Failed to load config: " + std::string(e.what()));
            LOG_INFO("Using default configuration");
        }
    } else {
        // Try RTVORTEX_HOME/config/default.yml as fallback
        std::string fallback = (fs::path(g_env.config_dir) / "default.yml").string();
        if (fileExists(fallback)) {
            try {
                engine_config = aipr::EngineConfig::load(fallback);
                LOG_INFO("Config loaded from: " + fallback);
            } catch (const std::exception& e) {
                LOG_WARN("Failed to load fallback config: " + std::string(e.what()));
                LOG_INFO("Using default configuration");
            }
        } else {
            LOG_INFO("No config file found, using defaults");
        }
    }
    
    // Resolve model path relative to RTVORTEX_HOME if not absolute
    if (!engine_config.onnx_model_path.empty() &&
        !fs::path(engine_config.onnx_model_path).is_absolute()) {
        std::string resolved = (fs::path(g_env.models_dir) / 
            fs::path(engine_config.onnx_model_path).filename()).string();
        if (fileExists(resolved)) {
            engine_config.onnx_model_path = resolved;
            LOG_INFO("ONNX model: " + resolved);
        } else {
            LOG_WARN("ONNX model not found: " + resolved);
            LOG_WARN("Download from: https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2");
        }
    }
    
    // Point storage_path into RTVORTEX_HOME/data so the engine writes
    // index/TMS data inside the managed directory tree, not a relative ".rtvortex/" folder.
    engine_config.storage_path = (fs::path(g_env.data_dir) / "index").string();
    LOG_INFO("Storage path: " + engine_config.storage_path);

    // Create engine instance
    LOG_INFO("Initializing engine...");
    auto engine = aipr::Engine::create(engine_config);
    if (!engine) {
        throw std::runtime_error("Failed to create engine instance");
    }
    LOG_INFO("Engine initialized (version: " + engine->getVersion() + ")");
    
    // Create service implementation
    aipr::server::EngineServiceImpl service(std::move(engine));
    
    // Enable gRPC reflection for debugging
    grpc::reflection::InitProtoReflectionServerBuilderPlugin();
    
    // Enable built-in health check
    grpc::EnableDefaultHealthCheckService(true);
    
    // Build server
    grpc::ServerBuilder builder;
    
    // Add listening port with credentials
    auto credentials = buildCredentials(config);
    builder.AddListeningPort(server_address, credentials);
    
    // Register service
    builder.RegisterService(&service);
    
    // Set server options
    builder.SetMaxReceiveMessageSize(64 * 1024 * 1024);  // 64 MB max message
    builder.SetMaxSendMessageSize(64 * 1024 * 1024);
    
    // Build and start
    g_server = builder.BuildAndStart();
    if (!g_server) {
        throw std::runtime_error("Failed to start gRPC server");
    }
    
    LOG_INFO("Server listening on " + server_address);
    LOG_INFO("TLS: " + std::string(config.tls_enabled ? "enabled" : "disabled"));
    LOG_INFO("Press Ctrl+C to shutdown");
    LOG_INFO("=========================================");
    
    // Dismiss splash screen now that we're fully initialized
    rtvortex::SplashScreen::dismiss();
    
    // Wait for shutdown
    g_server->Wait();
    
    LOG_INFO("Server shutdown complete");
}

}  // namespace

//=============================================================================
// Main Entry Point
//=============================================================================

int main(int argc, char* argv[]) {
    try {
        ServerConfig config = parseArgs(argc, argv);
        
        // Initialize environment directories and logging FIRST
        initEnvironment();
        
#ifdef _WIN32
        // Handle Windows service commands
        switch (config.mode) {
            case ServerConfig::Mode::Install:
                return installService() ? 0 : 1;
                
            case ServerConfig::Mode::Uninstall:
                return uninstallService() ? 0 : 1;
                
            case ServerConfig::Mode::Start:
                return startService() ? 0 : 1;
                
            case ServerConfig::Mode::Stop:
                return stopService() ? 0 : 1;
                
            case ServerConfig::Mode::Service:
                // Running as Windows service (called by SCM)
                if (!runAsService()) {
                    DWORD error = GetLastError();
                    if (error == ERROR_FAILED_SERVICE_CONTROLLER_CONNECT) {
                        // Not running from SCM, run as console app
                        LOG_INFO("Not started by SCM, running in console mode...");
                    } else {
                        LOG_ERROR("Failed to start as service: " + std::to_string(error));
                        return 1;
                    }
                } else {
                    return 0;  // Service handled
                }
                break;
                
            case ServerConfig::Mode::Run:
            default:
                // Fall through to normal execution
                break;
        }
#endif
        
        // Setup signal handlers for graceful shutdown
        setupSignalHandlers();
        
        // Run the server in foreground
        runServer(config);
        
        LOG_INFO("Engine process exiting normally");
        return 0;
        
    } catch (const std::exception& e) {
        LOG_FATAL(std::string("Unhandled exception: ") + e.what());
        return 1;
    }
}
