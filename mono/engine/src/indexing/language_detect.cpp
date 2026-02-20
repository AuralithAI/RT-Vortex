/**
 * AI PR Reviewer - Language Detection
 * 
 * Detects programming language from file extension and content.
 */

#include "types.h"
#include <string>
#include <vector>
#include <unordered_map>
#include <algorithm>

namespace aipr {

/**
 * Language registry with metadata
 */
class LanguageRegistry {
public:
    static LanguageRegistry& instance() {
        static LanguageRegistry registry;
        return registry;
    }
    
    const Language* getByExtension(const std::string& ext) const {
        auto it = by_extension_.find(ext);
        if (it != by_extension_.end()) {
            return &languages_[it->second];
        }
        return nullptr;
    }
    
    const Language* getById(const std::string& id) const {
        for (const auto& lang : languages_) {
            if (lang.id == id) {
                return &lang;
            }
        }
        return nullptr;
    }
    
    const std::vector<Language>& all() const {
        return languages_;
    }
    
private:
    LanguageRegistry() {
        initializeLanguages();
        buildIndex();
    }
    
    void initializeLanguages() {
        languages_ = {
            {"cpp", "C++", {".cpp", ".cc", ".cxx", ".c++", ".h", ".hpp", ".hxx", ".h++", ".hh"}},
            {"c", "C", {".c"}},
            {"java", "Java", {".java"}},
            {"python", "Python", {".py", ".pyw", ".pyi"}},
            {"javascript", "JavaScript", {".js", ".mjs", ".cjs", ".jsx"}},
            {"typescript", "TypeScript", {".ts", ".tsx", ".mts", ".cts"}},
            {"go", "Go", {".go"}},
            {"rust", "Rust", {".rs"}},
            {"ruby", "Ruby", {".rb", ".rake", ".gemspec"}},
            {"php", "PHP", {".php", ".phtml", ".php3", ".php4", ".php5"}},
            {"csharp", "C#", {".cs", ".csx"}},
            {"swift", "Swift", {".swift"}},
            {"kotlin", "Kotlin", {".kt", ".kts"}},
            {"scala", "Scala", {".scala", ".sc"}},
            {"objective-c", "Objective-C", {".m", ".mm"}},
            {"bash", "Bash", {".sh", ".bash", ".zsh", ".fish"}},
            {"powershell", "PowerShell", {".ps1", ".psm1", ".psd1"}},
            {"sql", "SQL", {".sql", ".psql", ".mysql"}},
            {"r", "R", {".r", ".R", ".Rmd"}},
            {"lua", "Lua", {".lua"}},
            {"perl", "Perl", {".pl", ".pm", ".pod"}},
            {"elixir", "Elixir", {".ex", ".exs"}},
            {"erlang", "Erlang", {".erl", ".hrl"}},
            {"clojure", "Clojure", {".clj", ".cljs", ".cljc", ".edn"}},
            {"haskell", "Haskell", {".hs", ".lhs"}},
            {"ocaml", "OCaml", {".ml", ".mli"}},
            {"fsharp", "F#", {".fs", ".fsi", ".fsx"}},
            {"dart", "Dart", {".dart"}},
            {"vue", "Vue", {".vue"}},
            {"svelte", "Svelte", {".svelte"}},
            {"json", "JSON", {".json", ".jsonc", ".json5"}},
            {"yaml", "YAML", {".yaml", ".yml"}},
            {"xml", "XML", {".xml", ".xsd", ".xsl", ".xslt", ".svg"}},
            {"html", "HTML", {".html", ".htm", ".xhtml"}},
            {"css", "CSS", {".css", ".scss", ".sass", ".less", ".styl"}},
            {"markdown", "Markdown", {".md", ".markdown", ".mdown", ".mkd"}},
            {"toml", "TOML", {".toml"}},
            {"dockerfile", "Dockerfile", {".dockerfile"}},
            {"terraform", "Terraform", {".tf", ".tfvars", ".hcl"}},
            {"protobuf", "Protocol Buffers", {".proto"}},
            {"graphql", "GraphQL", {".graphql", ".gql"}},
            {"cmake", "CMake", {".cmake"}},
            {"makefile", "Makefile", {}},
            {"groovy", "Groovy", {".groovy", ".gvy", ".gy", ".gsh"}},
            {"gradle", "Gradle", {".gradle"}},
        };
    }
    
    void buildIndex() {
        for (size_t i = 0; i < languages_.size(); ++i) {
            for (const auto& ext : languages_[i].extensions) {
                by_extension_[ext] = i;
            }
        }
    }
    
    std::vector<Language> languages_;
    std::unordered_map<std::string, size_t> by_extension_;
};

/**
 * Detect language from file path
 */
std::string detectLanguage(const std::string& path) {
    // Get filename
    auto slash_pos = path.rfind('/');
    if (slash_pos == std::string::npos) {
        slash_pos = path.rfind('\\');
    }
    std::string filename = (slash_pos != std::string::npos) 
        ? path.substr(slash_pos + 1) 
        : path;
    
    // Check special filenames first
    if (filename == "Dockerfile" || filename.find("Dockerfile.") == 0) {
        return "dockerfile";
    }
    if (filename == "Makefile" || filename == "makefile" || filename == "GNUmakefile") {
        return "makefile";
    }
    if (filename == "CMakeLists.txt") {
        return "cmake";
    }
    if (filename == "Jenkinsfile") {
        return "groovy";
    }
    if (filename == ".gitignore" || filename == ".dockerignore") {
        return "gitignore";
    }
    
    // Get extension
    auto dot_pos = filename.rfind('.');
    if (dot_pos == std::string::npos || dot_pos == 0) {
        return "text";
    }
    
    std::string ext = filename.substr(dot_pos);
    std::transform(ext.begin(), ext.end(), ext.begin(), ::tolower);
    
    // Look up in registry
    auto& registry = LanguageRegistry::instance();
    auto lang = registry.getByExtension(ext);
    if (lang) {
        return lang->id;
    }
    
    return "text";
}

/**
 * Get language metadata by ID
 */
const Language* getLanguageInfo(const std::string& id) {
    return LanguageRegistry::instance().getById(id);
}

/**
 * Get all supported languages
 */
std::vector<Language> getSupportedLanguages() {
    return LanguageRegistry::instance().all();
}

} // namespace aipr
