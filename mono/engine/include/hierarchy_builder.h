/**
 * Hierarchy Builder — Extracts repository-level structural metadata
 *
 * Parses build manifests and produces a lightweight RepoManifest plus
 * per-file summary chunks.
 *
 * Supported build systems:
 *   cmake (CMakeLists.txt), make (Makefile), gradle (build.gradle[.kts]),
 *   maven (pom.xml), npm/yarn (package.json), go (go.mod),
 *   cargo (Cargo.toml), python (pyproject.toml / setup.py / setup.cfg),
 *   bazel (BUILD / WORKSPACE), dotnet (*.csproj / *.sln),
 *   swift (Package.swift), mix (mix.exs), docker (Dockerfile)
 *
 * Consumers:
 *   - ChunkPrefixer reads the manifest to build context prefixes.
 *   - TMSMemorySystem calls buildRepoManifest() during ingestion when
 *     ChunkingConfig::hierarchy_enabled is true.
 */

#pragma once

#include "tms/tms_types.h"
#include <string>
#include <vector>
#include <unordered_map>

namespace aipr {

// ── Build-system descriptor ────────────────────────────────────────────────

struct BuildTarget {
    std::string name;                      // e.g. "aipr-engine"
    std::string type;                      // "library", "executable", "module", "image", "package"
    std::vector<std::string> source_globs; // relative paths / globs
};

struct RepoManifest {
    std::string repo_root;
    std::string primary_language;          // majority language
    std::string build_system;              // see supported list above
    std::vector<BuildTarget> targets;

    // module → list of relative source paths that belong to it
    std::unordered_map<std::string, std::vector<std::string>> module_to_files;

    // file_path (relative) → module name
    std::unordered_map<std::string, std::string> file_to_module;
};

// ── Per-file summary ───────────────────────────────────────────────────────

struct FileSummary {
    std::string file_path;                 // relative
    std::string language;
    std::string module;                    // from manifest (may be empty)
    std::vector<std::string> top_symbols;  // exported / public names
    std::vector<std::string> imports;
    size_t line_count = 0;
};

// ── HierarchyBuilder ──────────────────────────────────────────────────────

class HierarchyBuilder {
public:
    HierarchyBuilder() = default;

    /**
     * Scan the repo root for build manifests and produce a RepoManifest.
     * Probes manifests in priority order (most specific first).
     */
    RepoManifest buildRepoManifest(const std::string& repo_root) const;

    /**
     * Create a lightweight FileSummary for one parsed file.
     */
    FileSummary summarizeFile(
        const std::string& file_path,
        const std::string& language,
        const std::vector<tms::CodeChunk>& file_chunks,
        const RepoManifest& manifest) const;

    /**
     * Turn a FileSummary into a FILE_SUMMARY CodeChunk ready for embedding.
     */
    tms::CodeChunk buildFileSummaryChunk(const FileSummary& summary) const;

private:
    // Manifest parsers — one per build system
    void parseCMakeLists(const std::string& path, RepoManifest& out) const;
    void parseMakefile(const std::string& path, RepoManifest& out) const;
    void parsePackageJson(const std::string& path, RepoManifest& out) const;
    void parseGoMod(const std::string& path, RepoManifest& out) const;
    void parsePomXml(const std::string& path, RepoManifest& out) const;
    void parseDockerfile(const std::string& path, RepoManifest& out) const;
    void parseGradle(const std::string& path, RepoManifest& out) const;
    void parseCargoToml(const std::string& path, RepoManifest& out) const;
    void parsePyProjectToml(const std::string& path, RepoManifest& out) const;
    void parseSetupPy(const std::string& path, RepoManifest& out) const;
    void parseBazel(const std::string& path, RepoManifest& out) const;
    void parseDotnet(const std::string& path, RepoManifest& out) const;
    void parseSwiftPackage(const std::string& path, RepoManifest& out) const;
    void parseMixExs(const std::string& path, RepoManifest& out) const;
};

} // namespace aipr
