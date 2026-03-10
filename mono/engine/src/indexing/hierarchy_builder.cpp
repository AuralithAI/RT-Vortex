/**
 * Hierarchy Builder implementation
 *
 * Scans a repository root for common build manifests and produces a
 * lightweight RepoManifest that drives hierarchical context prefixing.
 */

#include "hierarchy_builder.h"
#include "logging.h"
#include "metrics.h"
#include <nlohmann/json.hpp>
#include <filesystem>
#include <fstream>
#include <sstream>
#include <regex>
#include <algorithm>

namespace fs = std::filesystem;
using json = nlohmann::json;

namespace aipr {

// ── repoTypeName ───────────────────────────────────────────────────────────

const char* repoTypeName(RepoType t) {
    switch (t) {
        case RepoType::UNKNOWN:        return "unknown";
        case RepoType::GENERIC:        return "generic";
        case RepoType::WEB_APP:        return "web_app";
        case RepoType::MICROSERVICE:   return "microservice";
        case RepoType::ML_PIPELINE:    return "ml_pipeline";
        case RepoType::MOBILE_APP:     return "mobile_app";
        case RepoType::DATA_PIPELINE:  return "data_pipeline";
        case RepoType::CLI_TOOL:       return "cli_tool";
        case RepoType::LIBRARY:        return "library";
        case RepoType::MONOLITH:       return "monolith";
    }
    return "unknown";
}

// ── Helpers ────────────────────────────────────────────────────────────────

static std::string readFile(const std::string& path) {
    std::ifstream f(path);
    if (!f.good()) return {};
    std::ostringstream ss;
    ss << f.rdbuf();
    return ss.str();
}

static std::string detectPrimaryLanguage(const std::string& root) {
    std::unordered_map<std::string, int> ext_counts;
    for (auto& entry : fs::recursive_directory_iterator(
            root, fs::directory_options::skip_permission_denied)) {
        if (!entry.is_regular_file()) continue;
        auto ext = entry.path().extension().string();
        if (!ext.empty()) ext_counts[ext]++;
    }
    std::string best;
    int best_count = 0;
    static const std::unordered_map<std::string, std::string> ext_to_lang = {
        {".cpp", "cpp"}, {".cc", "cpp"}, {".h", "cpp"}, {".hpp", "cpp"}, {".cxx", "cpp"},
        {".c", "c"},
        {".py", "python"}, {".pyx", "python"},
        {".java", "java"},
        {".go", "go"},
        {".js", "javascript"}, {".jsx", "javascript"}, {".mjs", "javascript"},
        {".ts", "typescript"}, {".tsx", "typescript"},
        {".rs", "rust"},
        {".rb", "ruby"},
        {".cs", "csharp"},
        {".swift", "swift"},
        {".kt", "kotlin"}, {".kts", "kotlin"},
        {".scala", "scala"},
        {".ex", "elixir"}, {".exs", "elixir"},
        {".lua", "lua"},
        {".php", "php"},
        {".dart", "dart"},
        {".zig", "zig"},
    };
    for (auto& [ext, cnt] : ext_counts) {
        auto it = ext_to_lang.find(ext);
        if (it != ext_to_lang.end() && cnt > best_count) {
            best = it->second;
            best_count = cnt;
        }
    }
    return best.empty() ? "unknown" : best;
}

// ── buildRepoManifest ──────────────────────────────────────────────────────

RepoManifest HierarchyBuilder::buildRepoManifest(const std::string& repo_root) const {
    RepoManifest m;
    m.repo_root = repo_root;
    m.primary_language = detectPrimaryLanguage(repo_root);

    auto probe = [&](const std::string& name) {
        return (fs::path(repo_root) / name).string();
    };

    // Probe manifests in priority order (most specific → most generic).
    // Only the first match wins so ordering matters.
    if      (fs::exists(probe("CMakeLists.txt")))   parseCMakeLists(probe("CMakeLists.txt"), m);
    else if (fs::exists(probe("Cargo.toml")))        parseCargoToml(probe("Cargo.toml"), m);
    else if (fs::exists(probe("go.mod")))             parseGoMod(probe("go.mod"), m);
    else if (fs::exists(probe("pom.xml")))            parsePomXml(probe("pom.xml"), m);
    else if (fs::exists(probe("build.gradle.kts")))   parseGradle(probe("build.gradle.kts"), m);
    else if (fs::exists(probe("build.gradle")))        parseGradle(probe("build.gradle"), m);
    else if (fs::exists(probe("Package.swift")))       parseSwiftPackage(probe("Package.swift"), m);
    else if (fs::exists(probe("mix.exs")))             parseMixExs(probe("mix.exs"), m);
    else if (fs::exists(probe("pyproject.toml")))      parsePyProjectToml(probe("pyproject.toml"), m);
    else if (fs::exists(probe("setup.py")))            parseSetupPy(probe("setup.py"), m);
    else if (fs::exists(probe("setup.cfg")))           parseSetupPy(probe("setup.cfg"), m);
    else if (fs::exists(probe("package.json")))        parsePackageJson(probe("package.json"), m);
    else if (fs::exists(probe("WORKSPACE")))           parseBazel(probe("WORKSPACE"), m);
    else if (fs::exists(probe("WORKSPACE.bazel")))     parseBazel(probe("WORKSPACE.bazel"), m);
    else if (fs::exists(probe("Makefile")))            parseMakefile(probe("Makefile"), m);
    else {
        // Scan for .csproj / .sln in root
        bool found_dotnet = false;
        for (auto& entry : fs::directory_iterator(repo_root)) {
            auto ext = entry.path().extension().string();
            if (ext == ".csproj" || ext == ".sln" || ext == ".fsproj" || ext == ".vbproj") {
                parseDotnet(entry.path().string(), m);
                found_dotnet = true;
                break;
            }
        }
        if (!found_dotnet) {
            if (fs::exists(probe("Dockerfile")))   parseDockerfile(probe("Dockerfile"), m);
            else                                    m.build_system = "unknown";
        }
    }

    LOG_INFO("[HierarchyBuilder] manifest: build_system=" + m.build_system +
             " targets=" + std::to_string(m.targets.size()) +
             " modules=" + std::to_string(m.module_to_files.size()));

    // Auto-detect repo type from manifest signals
    m.repo_type = detectRepoType(m, repo_root);
    LOG_INFO("[HierarchyBuilder] repo_type=" + std::string(repoTypeName(m.repo_type)));

    return m;
}

// ── detectRepoType ─────────────────────────────────────────────────────────

RepoType HierarchyBuilder::detectRepoType(const RepoManifest& manifest,
                                           const std::string& repo_root) {
    const auto& lang = manifest.primary_language;
    const auto& bs   = manifest.build_system;
    bool has_docker   = fs::exists(fs::path(repo_root) / "Dockerfile");

    // ── Helpers: check for marker files ────────────────────────────────────
    auto has_file = [&](const std::string& name) {
        return fs::exists(fs::path(repo_root) / name);
    };

    // Count top-level directories as a monolith signal
    int top_dirs = 0;
    int top_build_files = 0;
    try {
        for (auto& entry : fs::directory_iterator(repo_root)) {
            if (entry.is_directory()) {
                auto dir_name = entry.path().filename().string();
                if (dir_name[0] != '.') top_dirs++;
            }
            auto ext = entry.path().extension().string();
            auto fname = entry.path().filename().string();
            if (fname == "pom.xml" || fname == "build.gradle" ||
                fname == "build.gradle.kts" || fname == "package.json" ||
                fname == "go.mod" || fname == "Cargo.toml" ||
                fname == "CMakeLists.txt") {
                top_build_files++;
            }
        }
    } catch (...) {}

    // ── ML / Data Pipeline ─────────────────────────────────────────────────
    if (lang == "python") {
        bool ml_signal = has_file("requirements.txt") || has_file("setup.py") ||
                         has_file("pyproject.toml");
        // Look for ML-specific markers
        if (ml_signal) {
            // Check for ML framework deps in pyproject.toml or requirements.txt
            for (const auto& name : {"requirements.txt", "pyproject.toml", "setup.py"}) {
                auto path = (fs::path(repo_root) / name).string();
                std::ifstream f(path);
                if (!f.good()) continue;
                std::string content((std::istreambuf_iterator<char>(f)),
                                     std::istreambuf_iterator<char>());
                auto lower_content = content;
                std::transform(lower_content.begin(), lower_content.end(),
                               lower_content.begin(), ::tolower);
                if (lower_content.find("torch") != std::string::npos ||
                    lower_content.find("tensorflow") != std::string::npos ||
                    lower_content.find("keras") != std::string::npos ||
                    lower_content.find("scikit") != std::string::npos ||
                    lower_content.find("transformers") != std::string::npos ||
                    lower_content.find("onnx") != std::string::npos) {
                    return RepoType::ML_PIPELINE;
                }
                if (lower_content.find("airflow") != std::string::npos ||
                    lower_content.find("luigi") != std::string::npos ||
                    lower_content.find("dagster") != std::string::npos ||
                    lower_content.find("prefect") != std::string::npos ||
                    lower_content.find("pyspark") != std::string::npos) {
                    return RepoType::DATA_PIPELINE;
                }
            }
        }
    }

    // ── Mobile App ─────────────────────────────────────────────────────────
    if (lang == "swift" || lang == "kotlin" || lang == "dart") {
        if (has_file("Package.swift") || has_file("Podfile") ||
            has_file("pubspec.yaml") || has_file("app/build.gradle") ||
            has_file("app/build.gradle.kts")) {
            return RepoType::MOBILE_APP;
        }
    }

    // ── Web App (frontend frameworks) ──────────────────────────────────────
    // npm build system already implies a JS/TS-ecosystem project (vue, svelte,
    // etc. files don't appear in the ext_to_lang map, so primary_language can
    // be "unknown" for framework-centric repos).  Gate only on build system.
    if (bs == "npm") {
        auto pkg_path = (fs::path(repo_root) / "package.json").string();
        std::ifstream f(pkg_path);
        if (f.good()) {
            std::string content((std::istreambuf_iterator<char>(f)),
                                 std::istreambuf_iterator<char>());
            if (content.find("\"next\"") != std::string::npos ||
                content.find("\"react\"") != std::string::npos ||
                content.find("\"vue\"") != std::string::npos ||
                content.find("\"angular\"") != std::string::npos ||
                content.find("\"svelte\"") != std::string::npos ||
                content.find("\"nuxt\"") != std::string::npos) {
                return RepoType::WEB_APP;
            }
        }
        // Pure CLI tool (no framework, has "bin" field)
        std::ifstream f2(pkg_path);
        if (f2.good()) {
            std::string content((std::istreambuf_iterator<char>(f2)),
                                 std::istreambuf_iterator<char>());
            if (content.find("\"bin\"") != std::string::npos) {
                return RepoType::CLI_TOOL;
            }
        }
    }

    // ── Microservice ───────────────────────────────────────────────────────
    if (has_docker && (bs == "go" || bs == "cargo" || bs == "python" ||
                       bs == "maven" || bs == "gradle")) {
        // Small codebase + Dockerfile + single binary → microservice
        if (manifest.targets.size() <= 2) {
            return RepoType::MICROSERVICE;
        }
    }

    // ── Monolith (Java/Spring with many modules, or large multi-module) ───
    if ((bs == "maven" || bs == "gradle") && manifest.module_to_files.size() > 3) {
        return RepoType::MONOLITH;
    }
    // Multi-workspace npm monorepo
    if (bs == "npm" && manifest.module_to_files.size() > 3) {
        return RepoType::MONOLITH;
    }

    // ── Library ────────────────────────────────────────────────────────────
    if (bs == "cmake" || bs == "cargo") {
        // Library if the primary target is a library type
        for (const auto& t : manifest.targets) {
            if (t.type == "library" || t.type == "package") {
                return RepoType::LIBRARY;
            }
        }
    }

    // ── CLI Tool ───────────────────────────────────────────────────────────
    if (!has_docker && (bs == "go" || bs == "cargo" || bs == "cmake")) {
        // Go modules are recorded with type "module" by parseGoMod, not
        // "executable", so we also match on "module" for Go repos.
        for (const auto& t : manifest.targets) {
            if (t.type == "executable" || (bs == "go" && t.type == "module")) {
                if (manifest.targets.size() <= 2) {
                    return RepoType::CLI_TOOL;
                }
            }
        }
    }

    // ── Fallback ───────────────────────────────────────────────────────────
    if (bs == "unknown") return RepoType::UNKNOWN;
    return RepoType::GENERIC;
}

// ── summarizeFile ──────────────────────────────────────────────────────────

FileSummary HierarchyBuilder::summarizeFile(
    const std::string& file_path,
    const std::string& language,
    const std::vector<tms::CodeChunk>& file_chunks,
    const RepoManifest& manifest) const
{
    FileSummary s;
    s.file_path = file_path;
    s.language = language;

    auto mod_it = manifest.file_to_module.find(file_path);
    if (mod_it != manifest.file_to_module.end()) {
        s.module = mod_it->second;
    }

    for (const auto& c : file_chunks) {
        if (!c.name.empty()) {
            s.top_symbols.push_back(c.name);
        }
        for (const auto& dep : c.dependencies) {
            if (std::find(s.imports.begin(), s.imports.end(), dep) == s.imports.end()) {
                s.imports.push_back(dep);
            }
        }
        if (c.end_line > static_cast<int>(s.line_count)) {
            s.line_count = static_cast<size_t>(c.end_line);
        }
    }

    return s;
}

// ── buildFileSummaryChunk ──────────────────────────────────────────────────

tms::CodeChunk HierarchyBuilder::buildFileSummaryChunk(const FileSummary& summary) const {
    tms::CodeChunk chunk;
    chunk.id = "file_summary:" + summary.file_path;
    chunk.file_path = summary.file_path;
    chunk.language = summary.language;
    chunk.type = "file_summary";
    chunk.name = summary.file_path;

    std::ostringstream content;
    content << "File: " << summary.file_path << "\n";
    content << "Language: " << summary.language << "\n";
    if (!summary.module.empty()) {
        content << "Module: " << summary.module << "\n";
    }
    content << "Lines: " << summary.line_count << "\n";

    if (!summary.top_symbols.empty()) {
        content << "Symbols:";
        for (size_t i = 0; i < summary.top_symbols.size() && i < 20; ++i) {
            content << " " << summary.top_symbols[i];
        }
        content << "\n";
    }

    if (!summary.imports.empty()) {
        content << "Imports:";
        for (size_t i = 0; i < summary.imports.size() && i < 10; ++i) {
            content << " " << summary.imports[i];
        }
        content << "\n";
    }

    chunk.content = content.str();
    chunk.start_line = 0;
    chunk.end_line = static_cast<int>(summary.line_count);
    chunk.importance_score = 0.8;  // file summaries are useful context

    metrics::Registry::instance().incCounter(metrics::HIERARCHY_CHUNKS_TOTAL);

    return chunk;
}

// ── Manifest parsers ───────────────────────────────────────────────────────

void HierarchyBuilder::parseCMakeLists(const std::string& path, RepoManifest& out) const {
    out.build_system = "cmake";
    std::string content = readFile(path);
    if (content.empty()) return;

    // Extract add_library / add_executable targets
    std::regex target_re(R"((?:add_library|add_executable)\s*\(\s*(\S+))");
    auto it = std::sregex_iterator(content.begin(), content.end(), target_re);
    auto end = std::sregex_iterator();
    for (; it != end; ++it) {
        BuildTarget t;
        t.name = (*it)[1].str();
        t.type = content.find("add_library") != std::string::npos ? "library" : "executable";
        out.targets.push_back(std::move(t));
    }

    // Extract set(...SOURCES ...) blocks
    std::regex src_re(R"(set\s*\(\s*(\w+SOURCES)\s+([\s\S]*?)\))");
    it = std::sregex_iterator(content.begin(), content.end(), src_re);
    for (; it != end; ++it) {
        std::string var_name = (*it)[1].str();
        std::string body = (*it)[2].str();

        // Extract individual source paths
        std::regex file_re(R"(([\w/\.\-]+\.(?:cpp|c|cc|h|hpp)))");
        auto fit = std::sregex_iterator(body.begin(), body.end(), file_re);
        for (; fit != std::sregex_iterator(); ++fit) {
            std::string src = (*fit)[1].str();
            out.module_to_files[var_name].push_back(src);
            out.file_to_module[src] = var_name;
        }
    }
}

void HierarchyBuilder::parseMakefile(const std::string& path, RepoManifest& out) const {
    out.build_system = "make";
    std::string content = readFile(path);
    if (content.empty()) return;

    // Simple extraction of SRC/OBJ variables
    std::regex src_re(R"((\w+)\s*[:+]?=\s*(.*\.(?:cpp|c|cc|o|h)\b.*))", std::regex::multiline);
    auto it = std::sregex_iterator(content.begin(), content.end(), src_re);
    for (; it != std::sregex_iterator(); ++it) {
        std::string var = (*it)[1].str();
        std::string body = (*it)[2].str();

        std::regex file_re(R"(([\w/\.\-]+\.(?:cpp|c|cc|h|hpp)))");
        auto fit = std::sregex_iterator(body.begin(), body.end(), file_re);
        for (; fit != std::sregex_iterator(); ++fit) {
            out.module_to_files[var].push_back((*fit)[1].str());
            out.file_to_module[(*fit)[1].str()] = var;
        }
    }
}

void HierarchyBuilder::parsePackageJson(const std::string& path, RepoManifest& out) const {
    out.build_system = "npm";
    std::string content = readFile(path);
    if (content.empty()) return;

    try {
        json j = json::parse(content);
        if (j.contains("name")) {
            BuildTarget t;
            t.name = j["name"].get<std::string>();
            t.type = "module";
            out.targets.push_back(std::move(t));
        }
        // Workspaces → modules
        if (j.contains("workspaces")) {
            for (auto& ws : j["workspaces"]) {
                std::string workspace = ws.get<std::string>();
                out.module_to_files[workspace] = {};
            }
        }
    } catch (...) {
        LOG_WARN("[HierarchyBuilder] failed to parse package.json");
    }
}

void HierarchyBuilder::parseGoMod(const std::string& path, RepoManifest& out) const {
    out.build_system = "go";
    std::string content = readFile(path);
    if (content.empty()) return;

    std::regex mod_re(R"(^module\s+(\S+))");
    std::smatch match;
    if (std::regex_search(content, match, mod_re)) {
        BuildTarget t;
        t.name = match[1].str();
        t.type = "module";
        out.targets.push_back(std::move(t));
    }
}

void HierarchyBuilder::parsePomXml(const std::string& path, RepoManifest& out) const {
    out.build_system = "maven";
    std::string content = readFile(path);
    if (content.empty()) return;

    // Simple regex for <artifactId>
    std::regex art_re(R"(<artifactId>([^<]+)</artifactId>)");
    std::smatch match;
    if (std::regex_search(content, match, art_re)) {
        BuildTarget t;
        t.name = match[1].str();
        t.type = "module";
        out.targets.push_back(std::move(t));
    }
}

void HierarchyBuilder::parseDockerfile(const std::string& path, RepoManifest& out) const {
    out.build_system = "docker";
    std::string content = readFile(path);
    if (content.empty()) return;

    // Extract base image
    std::regex from_re(R"(^FROM\s+(\S+))", std::regex::multiline);
    std::smatch match;
    if (std::regex_search(content, match, from_re)) {
        BuildTarget t;
        t.name = match[1].str();
        t.type = "image";
        out.targets.push_back(std::move(t));
    }
}

// ── Gradle (build.gradle / build.gradle.kts) ──────────────────────────────

void HierarchyBuilder::parseGradle(const std::string& path, RepoManifest& out) const {
    out.build_system = "gradle";
    std::string content = readFile(path);
    if (content.empty()) return;

    bool found_name = false;

    // First check settings.gradle(.kts) for rootProject.name and subprojects
    auto settings_dir = fs::path(path).parent_path();
    for (const auto& name : {"settings.gradle", "settings.gradle.kts"}) {
        auto sp = (settings_dir / name).string();
        std::string sc = readFile(sp);
        if (sc.empty()) continue;

        std::regex root_re(R"(rootProject\.name\s*=\s*['\"]([^'\"]+)['\"])");
        std::smatch rm;
        if (std::regex_search(sc, rm, root_re)) {
            BuildTarget t;
            t.name = rm[1].str();
            t.type = "module";
            out.targets.push_back(std::move(t));
            found_name = true;
        }

        std::regex include_re(R"(include\s*\(?['\"]([^'\"]+)['\"])");
        auto it = std::sregex_iterator(sc.begin(), sc.end(), include_re);
        for (; it != std::sregex_iterator(); ++it) {
            std::string module = (*it)[1].str();
            if (!module.empty() && module[0] == ':') module = module.substr(1);
            out.module_to_files[module] = {};
        }
    }

    // Fallback: try rootProject.name in build.gradle itself
    if (!found_name) {
        std::regex root_re(R"(rootProject\.name\s*=\s*['\"]([^'\"]+)['\"])");
        std::smatch match;
        if (std::regex_search(content, match, root_re)) {
            BuildTarget t;
            t.name = match[1].str();
            t.type = "module";
            out.targets.push_back(std::move(t));
            found_name = true;
        }
    }

    // Fallback: use group id from build.gradle
    if (!found_name) {
        std::regex group_re(R"(group\s*=?\s*['\"]([^'\"]+)['\"])");
        std::smatch match;
        if (std::regex_search(content, match, group_re)) {
            BuildTarget t;
            t.name = match[1].str();
            t.type = "module";
            out.targets.push_back(std::move(t));
        }
    }
}

// ── Cargo (Cargo.toml) ────────────────────────────────────────────────────

void HierarchyBuilder::parseCargoToml(const std::string& path, RepoManifest& out) const {
    out.build_system = "cargo";
    std::string content = readFile(path);
    if (content.empty()) return;

    // Extract [package] name
    std::regex name_re(R"(name\s*=\s*\"([^\"]+)\")");
    std::smatch match;
    if (std::regex_search(content, match, name_re)) {
        BuildTarget t;
        t.name = match[1].str();
        t.type = "package";
        out.targets.push_back(std::move(t));
    }

    // Detect workspace members
    std::regex members_re(R"(members\s*=\s*\[([\s\S]*?)\])");
    if (std::regex_search(content, match, members_re)) {
        std::string body = match[1].str();
        std::regex member_re(R"(\"([^\"]+)\")");
        auto it = std::sregex_iterator(body.begin(), body.end(), member_re);
        for (; it != std::sregex_iterator(); ++it) {
            out.module_to_files[(*it)[1].str()] = {};
        }
    }
}

// ── Python (pyproject.toml) ───────────────────────────────────────────────

void HierarchyBuilder::parsePyProjectToml(const std::string& path, RepoManifest& out) const {
    out.build_system = "python";
    std::string content = readFile(path);
    if (content.empty()) return;

    // Extract project name from [project] or [tool.poetry]
    std::regex name_re(R"(name\s*=\s*\"([^\"]+)\")");
    std::smatch match;
    if (std::regex_search(content, match, name_re)) {
        BuildTarget t;
        t.name = match[1].str();
        t.type = "package";
        out.targets.push_back(std::move(t));
    }

    // Detect packages = ["src/..."]
    std::regex pkg_re(R"(packages\s*=\s*\[([\s\S]*?)\])");
    if (std::regex_search(content, match, pkg_re)) {
        std::string body = match[1].str();
        std::regex item_re(R"(\"([^\"]+)\")");
        auto it = std::sregex_iterator(body.begin(), body.end(), item_re);
        for (; it != std::sregex_iterator(); ++it) {
            out.module_to_files[(*it)[1].str()] = {};
        }
    }
}

// ── Python (setup.py / setup.cfg) ─────────────────────────────────────────

void HierarchyBuilder::parseSetupPy(const std::string& path, RepoManifest& out) const {
    out.build_system = "python";
    std::string content = readFile(path);
    if (content.empty()) return;

    // Extract name= from setup() call or [metadata]
    std::regex name_re(R"(name\s*=\s*['\"]([^'\"]+)['\"])");
    std::smatch match;
    if (std::regex_search(content, match, name_re)) {
        BuildTarget t;
        t.name = match[1].str();
        t.type = "package";
        out.targets.push_back(std::move(t));
    }
}

// ── Bazel (WORKSPACE / BUILD) ─────────────────────────────────────────────

void HierarchyBuilder::parseBazel(const std::string& path, RepoManifest& out) const {
    out.build_system = "bazel";
    std::string content = readFile(path);
    if (content.empty()) return;

    // Extract workspace name
    std::regex ws_re(R"(workspace\s*\(\s*name\s*=\s*\"([^\"]+)\")");
    std::smatch match;
    if (std::regex_search(content, match, ws_re)) {
        BuildTarget t;
        t.name = match[1].str();
        t.type = "module";
        out.targets.push_back(std::move(t));
    }

    // Scan for BUILD files to discover packages
    auto root = fs::path(path).parent_path();
    for (auto& entry : fs::recursive_directory_iterator(
            root, fs::directory_options::skip_permission_denied)) {
        if (!entry.is_regular_file()) continue;
        auto fname = entry.path().filename().string();
        if (fname == "BUILD" || fname == "BUILD.bazel") {
            auto rel = fs::relative(entry.path().parent_path(), root).string();
            if (!rel.empty() && rel != ".") {
                out.module_to_files[rel] = {};
            }
        }
    }
}

// ── .NET (*.csproj / *.sln / *.fsproj) ────────────────────────────────────

void HierarchyBuilder::parseDotnet(const std::string& path, RepoManifest& out) const {
    out.build_system = "dotnet";
    std::string content = readFile(path);
    if (content.empty()) return;

    auto ext = fs::path(path).extension().string();
    if (ext == ".sln") {
        // Extract Project("...") = "Name", "path"
        std::regex proj_re(R"(Project\([^\)]+\)\s*=\s*\"([^\"]+)\"\s*,\s*\"([^\"]+)\")");
        auto it = std::sregex_iterator(content.begin(), content.end(), proj_re);
        for (; it != std::sregex_iterator(); ++it) {
            BuildTarget t;
            t.name = (*it)[1].str();
            t.type = "module";
            out.targets.push_back(std::move(t));

            std::string proj_path = (*it)[2].str();
            out.module_to_files[t.name] = {};
        }
    } else {
        // .csproj / .fsproj / .vbproj — extract <RootNamespace> or <AssemblyName>
        std::regex asm_re(R"(<(?:AssemblyName|RootNamespace)>([^<]+)</)");
        std::smatch match;
        if (std::regex_search(content, match, asm_re)) {
            BuildTarget t;
            t.name = match[1].str();
            t.type = "module";
            out.targets.push_back(std::move(t));
        } else {
            // Fallback: use filename without extension
            BuildTarget t;
            t.name = fs::path(path).stem().string();
            t.type = "module";
            out.targets.push_back(std::move(t));
        }
    }
}

// ── Swift (Package.swift) ─────────────────────────────────────────────────

void HierarchyBuilder::parseSwiftPackage(const std::string& path, RepoManifest& out) const {
    out.build_system = "swift";
    std::string content = readFile(path);
    if (content.empty()) return;

    // Extract name: "..." from Package(name: "...")
    std::regex name_re(R"(name\s*:\s*\"([^\"]+)\")");
    std::smatch match;
    if (std::regex_search(content, match, name_re)) {
        BuildTarget t;
        t.name = match[1].str();
        t.type = "package";
        out.targets.push_back(std::move(t));
    }

    // Extract .target(name: "...") entries
    std::regex target_re(R"(\.(?:target|executableTarget|testTarget)\s*\(\s*name\s*:\s*\"([^\"]+)\")");
    auto it = std::sregex_iterator(content.begin(), content.end(), target_re);
    for (; it != std::sregex_iterator(); ++it) {
        std::string mod = (*it)[1].str();
        out.module_to_files[mod] = {};
    }
}

// ── Elixir/Mix (mix.exs) ─────────────────────────────────────────────────

void HierarchyBuilder::parseMixExs(const std::string& path, RepoManifest& out) const {
    out.build_system = "mix";
    std::string content = readFile(path);
    if (content.empty()) return;

    // Extract app: :name from project()
    std::regex app_re(R"(app\s*:\s*:(\w+))");
    std::smatch match;
    if (std::regex_search(content, match, app_re)) {
        BuildTarget t;
        t.name = match[1].str();
        t.type = "package";
        out.targets.push_back(std::move(t));
    }
}

} // namespace aipr
