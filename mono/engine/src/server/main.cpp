/**
 * AI PR Reviewer - Engine gRPC Server Entry Point
 *
 * This is the main entry point for the standalone C++ engine gRPC server.
 * It handles:
 *   - CLI argument parsing (--host, --port, --config)
 *   - YAML config loading
 *   - TLS credential loading
 *   - Graceful shutdown on SIGTERM/SIGINT
 *   - Windows service control (install/uninstall/start/stop/run)
 *
 * Windows Service Commands:
 *   aipr-engine.exe install   - Install as Windows service
 *   aipr-engine.exe uninstall - Remove Windows service
 *   aipr-engine.exe start     - Start the service
 *   aipr-engine.exe stop      - Stop the service
 *   aipr-engine.exe run       - Run in foreground (default)
 */

#include "engine_service_impl.h"
#include "engine_api.h"

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

#ifdef _WIN32
#include <windows.h>
#include <winsvc.h>
#include <tchar.h>
#else
#include <unistd.h>
#endif

// Service constants
#ifdef _WIN32
#define SERVICE_NAME        TEXT("AIPREngine")
#define SERVICE_DISPLAY     TEXT("AIPR Indexing & Retrieval Engine")
#define SERVICE_DESCRIPTION TEXT("High-performance code indexing and retrieval engine for AI PR Reviewer")
#endif

namespace {

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
};

void printUsage(const char* program_name) {
    std::cout << "AI PR Reviewer - Engine gRPC Server\n"
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
              << "  --version           Print version and exit\n"
              << "  --help              Print this help and exit\n"
              << "\n"
              << "Environment variables:\n"
              << "  ENGINE_HOST         Override --host\n"
              << "  ENGINE_PORT         Override --port\n"
              << "  ENGINE_CONFIG       Override --config\n"
              << "  ENGINE_TLS_ENABLED  Enable TLS (true/false)\n"
              << "  ENGINE_TLS_CERT     Path to server certificate\n"
              << "  ENGINE_TLS_KEY      Path to server private key\n"
              << "  ENGINE_TLS_CA       Path to CA certificate (for mTLS)\n"
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
    
    // Parse command line arguments (override env vars)
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        
        if (arg == "--help" || arg == "-h") {
            printUsage(argv[0]);
            std::exit(0);
        }
        else if (arg == "--version" || arg == "-v") {
            std::cout << "aipr-engine version 0.1.0\n";
            std::exit(0);
        }
        else if (arg == "--verbose") {
            config.verbose = true;
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
        std::cout << "[INFO] TLS disabled, using insecure credentials\n";
        return grpc::InsecureServerCredentials();
    }
    
    std::cout << "[INFO] TLS enabled, loading certificates...\n";
    
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
            std::cout << "[INFO] mTLS enabled (client certificate required)\n";
        } else {
            ssl_opts.client_certificate_request = 
                GRPC_SSL_REQUEST_CLIENT_CERTIFICATE_BUT_DONT_VERIFY;
            std::cout << "[INFO] TLS with optional client certificate\n";
        }
    } else {
        ssl_opts.client_certificate_request = GRPC_SSL_DONT_REQUEST_CLIENT_CERTIFICATE;
        std::cout << "[INFO] TLS enabled (server-side only)\n";
    }
    
    return grpc::SslServerCredentials(ssl_opts);
}

//=============================================================================
// Signal Handling
//=============================================================================

void signalHandler(int signal) {
    std::cout << "\n[INFO] Received signal " << signal << ", initiating graceful shutdown...\n";
    g_shutdown_requested = true;
    
    if (g_server) {
        // Graceful shutdown with deadline
        auto deadline = std::chrono::system_clock::now() + std::chrono::seconds(30);
        g_server->Shutdown(deadline);
    }
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
        std::cerr << "[ERROR] Cannot open Service Control Manager. Run as Administrator.\n";
        return false;
    }
    
    std::string exePath = getExecutablePath();
    std::string cmdLine = "\"" + exePath + "\" --service";
    
    SC_HANDLE service = CreateServiceA(
        scm,
        "AIPREngine",                      // Service name
        "AIPR Indexing & Retrieval Engine", // Display name
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
            std::cout << "[INFO] Service already exists.\n";
        } else {
            std::cerr << "[ERROR] CreateService failed: " << error << "\n";
        }
        CloseServiceHandle(scm);
        return error == ERROR_SERVICE_EXISTS;
    }
    
    // Set service description
    SERVICE_DESCRIPTIONA desc;
    desc.lpDescription = const_cast<LPSTR>(
        "High-performance code indexing and retrieval engine for AI PR Reviewer. "
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
    
    std::cout << "[OK] Service 'AIPREngine' installed successfully.\n";
    std::cout << "     Start with: aipr-engine start\n";
    std::cout << "     Or: sc start AIPREngine\n";
    
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
        std::cerr << "[ERROR] Cannot open Service Control Manager. Run as Administrator.\n";
        return false;
    }
    
    SC_HANDLE service = OpenServiceA(scm, "AIPREngine", SERVICE_STOP | DELETE | SERVICE_QUERY_STATUS);
    if (!service) {
        DWORD error = GetLastError();
        if (error == ERROR_SERVICE_DOES_NOT_EXIST) {
            std::cout << "[INFO] Service does not exist.\n";
        } else {
            std::cerr << "[ERROR] OpenService failed: " << error << "\n";
        }
        CloseServiceHandle(scm);
        return error == ERROR_SERVICE_DOES_NOT_EXIST;
    }
    
    // Stop service if running
    SERVICE_STATUS status;
    if (QueryServiceStatus(service, &status) && status.dwCurrentState != SERVICE_STOPPED) {
        std::cout << "[INFO] Stopping service...\n";
        ControlService(service, SERVICE_CONTROL_STOP, &status);
        
        // Wait for stop
        int tries = 0;
        while (status.dwCurrentState != SERVICE_STOPPED && tries++ < 30) {
            Sleep(1000);
            QueryServiceStatus(service, &status);
        }
    }
    
    if (!DeleteService(service)) {
        std::cerr << "[ERROR] DeleteService failed: " << GetLastError() << "\n";
        CloseServiceHandle(service);
        CloseServiceHandle(scm);
        return false;
    }
    
    std::cout << "[OK] Service 'AIPREngine' uninstalled successfully.\n";
    
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
        std::cerr << "[ERROR] Cannot open Service Control Manager.\n";
        return false;
    }
    
    SC_HANDLE service = OpenServiceA(scm, "AIPREngine", SERVICE_START | SERVICE_QUERY_STATUS);
    if (!service) {
        DWORD error = GetLastError();
        if (error == ERROR_SERVICE_DOES_NOT_EXIST) {
            std::cerr << "[ERROR] Service not installed. Run: aipr-engine install\n";
        } else {
            std::cerr << "[ERROR] OpenService failed: " << error << "\n";
        }
        CloseServiceHandle(scm);
        return false;
    }
    
    SERVICE_STATUS status;
    QueryServiceStatus(service, &status);
    
    if (status.dwCurrentState == SERVICE_RUNNING) {
        std::cout << "[INFO] Service is already running.\n";
        CloseServiceHandle(service);
        CloseServiceHandle(scm);
        return true;
    }
    
    if (!StartServiceA(service, 0, NULL)) {
        DWORD error = GetLastError();
        if (error == ERROR_SERVICE_ALREADY_RUNNING) {
            std::cout << "[INFO] Service is already running.\n";
        } else {
            std::cerr << "[ERROR] StartService failed: " << error << "\n";
            CloseServiceHandle(service);
            CloseServiceHandle(scm);
            return false;
        }
    }
    
    std::cout << "[OK] Service 'AIPREngine' started.\n";
    
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
        std::cerr << "[ERROR] Cannot open Service Control Manager.\n";
        return false;
    }
    
    SC_HANDLE service = OpenServiceA(scm, "AIPREngine", SERVICE_STOP | SERVICE_QUERY_STATUS);
    if (!service) {
        DWORD error = GetLastError();
        if (error == ERROR_SERVICE_DOES_NOT_EXIST) {
            std::cerr << "[ERROR] Service not installed.\n";
        } else {
            std::cerr << "[ERROR] OpenService failed: " << error << "\n";
        }
        CloseServiceHandle(scm);
        return false;
    }
    
    SERVICE_STATUS status;
    QueryServiceStatus(service, &status);
    
    if (status.dwCurrentState == SERVICE_STOPPED) {
        std::cout << "[INFO] Service is already stopped.\n";
        CloseServiceHandle(service);
        CloseServiceHandle(scm);
        return true;
    }
    
    if (!ControlService(service, SERVICE_CONTROL_STOP, &status)) {
        std::cerr << "[ERROR] ControlService(STOP) failed: " << GetLastError() << "\n";
        CloseServiceHandle(service);
        CloseServiceHandle(scm);
        return false;
    }
    
    // Wait for stop
    std::cout << "[INFO] Stopping service...\n";
    int tries = 0;
    while (status.dwCurrentState != SERVICE_STOPPED && tries++ < 30) {
        Sleep(1000);
        QueryServiceStatus(service, &status);
        std::cout << "." << std::flush;
    }
    std::cout << "\n";
    
    if (status.dwCurrentState == SERVICE_STOPPED) {
        std::cout << "[OK] Service 'AIPREngine' stopped.\n";
    } else {
        std::cerr << "[WARN] Service may not have stopped cleanly.\n";
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
        "AIPREngine",
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
        { const_cast<LPSTR>("AIPREngine"), ServiceMain },
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
    
    std::cout << "=========================================\n";
    std::cout << " AI PR Reviewer - Engine gRPC Server\n";
    std::cout << "=========================================\n";
    std::cout << "[INFO] Loading config from: " << config.config_path << "\n";
    
    // Load engine config
    aipr::EngineConfig engine_config;
    if (fileExists(config.config_path)) {
        try {
            engine_config = aipr::EngineConfig::load(config.config_path);
            std::cout << "[INFO] Config loaded successfully\n";
        } catch (const std::exception& e) {
            std::cout << "[WARN] Failed to load config: " << e.what() << "\n";
            std::cout << "[INFO] Using default configuration\n";
        }
    } else {
        std::cout << "[INFO] Config file not found, using defaults\n";
    }
    
    // Create engine instance
    std::cout << "[INFO] Initializing engine...\n";
    auto engine = aipr::Engine::create(engine_config);
    if (!engine) {
        throw std::runtime_error("Failed to create engine instance");
    }
    std::cout << "[INFO] Engine initialized (version: " << engine->getVersion() << ")\n";
    
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
    
    std::cout << "[INFO] Server listening on " << server_address << "\n";
    std::cout << "[INFO] TLS: " << (config.tls_enabled ? "enabled" : "disabled") << "\n";
    std::cout << "[INFO] Press Ctrl+C to shutdown\n";
    std::cout << "=========================================\n";
    
    // Wait for shutdown
    g_server->Wait();
    
    std::cout << "[INFO] Server shutdown complete\n";
}

}  // namespace

//=============================================================================
// Main Entry Point
//=============================================================================

int main(int argc, char* argv[]) {
    try {
        ServerConfig config = parseArgs(argc, argv);
        
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
                        std::cout << "[INFO] Not started by SCM, running in console mode...\n";
                    } else {
                        std::cerr << "[ERROR] Failed to start as service: " << error << "\n";
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
        
        return 0;
        
    } catch (const std::exception& e) {
        std::cerr << "[FATAL] " << e.what() << "\n";
        return 1;
    }
}
