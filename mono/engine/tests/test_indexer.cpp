/**
 * AI PR Reviewer Engine - Indexer Tests
 */

#include <gtest/gtest.h>
#include <fstream>
#include <filesystem>
#include "types.h"
#include "indexer.h"

namespace aipr {
namespace test {

namespace fs = std::filesystem;

class IndexerTest : public ::testing::Test {
protected:
    fs::path temp_dir_;
    
    void SetUp() override {
        temp_dir_ = fs::temp_directory_path() / "aipr_test_indexer";
        fs::create_directories(temp_dir_);
    }

    void TearDown() override {
        if (fs::exists(temp_dir_)) {
            fs::remove_all(temp_dir_);
        }
    }
    
    void createFile(const std::string& relative_path, const std::string& content) {
        fs::path full_path = temp_dir_ / relative_path;
        fs::create_directories(full_path.parent_path());
        std::ofstream file(full_path);
        file << content;
        file.close();
    }
};

// =============================================================================
// IndexerConfig Tests
// =============================================================================

TEST_F(IndexerTest, IndexerConfigDefaults) {
    IndexerConfig config;
    
    EXPECT_EQ(config.max_file_size_kb, 1024);
    EXPECT_EQ(config.chunk_size, 512);
    EXPECT_EQ(config.chunk_overlap, 64);
    EXPECT_TRUE(config.enable_ast_chunking);
    EXPECT_FALSE(config.tiered_enabled);
}

TEST_F(IndexerTest, IndexerConfigCustomValues) {
    IndexerConfig config;
    config.max_file_size_kb = 2048;
    config.chunk_size = 256;
    config.chunk_overlap = 32;
    config.include_patterns = {"*.cpp", "*.h"};
    config.exclude_patterns = {"*_test.cpp", "vendor/*"};
    
    EXPECT_EQ(config.max_file_size_kb, 2048);
    EXPECT_EQ(config.chunk_size, 256);
    EXPECT_EQ(config.include_patterns.size(), 2);
    EXPECT_EQ(config.exclude_patterns.size(), 2);
}

// =============================================================================
// Chunk Tests
// =============================================================================

TEST_F(IndexerTest, ChunkCreation) {
    Chunk chunk;
    chunk.id = "chunk_001";
    chunk.file_path = "src/main.cpp";
    chunk.start_line = 1;
    chunk.end_line = 50;
    chunk.content = "int main() { return 0; }";
    chunk.language = "cpp";
    
    EXPECT_EQ(chunk.id, "chunk_001");
    EXPECT_EQ(chunk.file_path, "src/main.cpp");
    EXPECT_EQ(chunk.start_line, 1);
    EXPECT_EQ(chunk.end_line, 50);
    EXPECT_EQ(chunk.language, "cpp");
}

TEST_F(IndexerTest, ChunkWithSymbols) {
    Chunk chunk;
    chunk.id = "chunk_002";
    chunk.file_path = "src/utils.cpp";
    chunk.symbols = {"calculateSum", "validateInput", "parseConfig"};
    chunk.imports = {"<iostream>", "<string>", "<vector>"};
    chunk.parent_symbol = "Utils";
    
    EXPECT_EQ(chunk.symbols.size(), 3);
    EXPECT_EQ(chunk.imports.size(), 3);
    EXPECT_TRUE(chunk.parent_symbol.has_value());
    EXPECT_EQ(chunk.parent_symbol.value(), "Utils");
}

TEST_F(IndexerTest, ChunkEmbedding) {
    Chunk chunk;
    chunk.id = "chunk_003";
    chunk.embedding = {0.1f, 0.2f, 0.3f, 0.4f, 0.5f};
    
    EXPECT_EQ(chunk.embedding.size(), 5);
    EXPECT_FLOAT_EQ(chunk.embedding[0], 0.1f);
    EXPECT_FLOAT_EQ(chunk.embedding[4], 0.5f);
}

// =============================================================================
// FileInfo Tests
// =============================================================================

TEST_F(IndexerTest, FileInfoDefaults) {
    FileInfo info;
    
    EXPECT_EQ(info.size_bytes, 0);
    EXPECT_EQ(info.line_count, 0);
    EXPECT_FALSE(info.is_binary);
    EXPECT_FALSE(info.is_generated);
}

TEST_F(IndexerTest, FileInfoPopulated) {
    FileInfo info;
    info.path = "src/engine.cpp";
    info.blob_sha = "abc123def456";
    info.language = "cpp";
    info.size_bytes = 4096;
    info.line_count = 150;
    info.is_binary = false;
    info.is_generated = false;
    
    EXPECT_EQ(info.path, "src/engine.cpp");
    EXPECT_EQ(info.blob_sha, "abc123def456");
    EXPECT_EQ(info.language, "cpp");
    EXPECT_EQ(info.size_bytes, 4096);
    EXPECT_EQ(info.line_count, 150);
}

// =============================================================================
// ManifestEntry Tests
// =============================================================================

TEST_F(IndexerTest, ManifestEntryCreation) {
    ManifestEntry entry;
    entry.file_path = "src/main.cpp";
    entry.blob_sha = "sha256:abc123";
    entry.chunk_ids = {"chunk_001", "chunk_002", "chunk_003"};
    entry.language = "cpp";
    entry.size_bytes = 2048;
    entry.last_indexed = "2026-02-19T10:00:00Z";
    
    EXPECT_EQ(entry.file_path, "src/main.cpp");
    EXPECT_EQ(entry.chunk_ids.size(), 3);
    EXPECT_EQ(entry.language, "cpp");
}

// =============================================================================
// IndexManifest Tests
// =============================================================================

TEST_F(IndexerTest, IndexManifestCreation) {
    IndexManifest manifest;
    manifest.repo_id = "repo_12345";
    manifest.version = "1.0.0";
    manifest.commit_sha = "def456789";
    manifest.created_at = "2026-02-19T09:00:00Z";
    manifest.updated_at = "2026-02-19T10:00:00Z";
    
    EXPECT_EQ(manifest.repo_id, "repo_12345");
    EXPECT_EQ(manifest.version, "1.0.0");
    EXPECT_EQ(manifest.commit_sha, "def456789");
    EXPECT_TRUE(manifest.entries.empty());
}

TEST_F(IndexerTest, IndexManifestWithEntries) {
    IndexManifest manifest;
    manifest.repo_id = "repo_12345";
    
    ManifestEntry entry1;
    entry1.file_path = "src/a.cpp";
    entry1.chunk_ids = {"c1", "c2"};
    
    ManifestEntry entry2;
    entry2.file_path = "src/b.cpp";
    entry2.chunk_ids = {"c3"};
    
    manifest.entries.push_back(entry1);
    manifest.entries.push_back(entry2);
    
    EXPECT_EQ(manifest.entries.size(), 2);
    EXPECT_EQ(manifest.entries[0].file_path, "src/a.cpp");
    EXPECT_EQ(manifest.entries[1].chunk_ids.size(), 1);
}

// =============================================================================
// Tiered Indexing Config Tests
// =============================================================================

TEST_F(IndexerTest, TieredIndexingConfig) {
    IndexerConfig config;
    config.tiered_enabled = true;
    config.hot_paths = {"src/core/", "src/api/"};
    config.cold_paths = {"docs/", "examples/"};
    
    EXPECT_TRUE(config.tiered_enabled);
    EXPECT_EQ(config.hot_paths.size(), 2);
    EXPECT_EQ(config.cold_paths.size(), 2);
    EXPECT_EQ(config.hot_paths[0], "src/core/");
}

// =============================================================================
// Language Filter Tests
// =============================================================================

TEST_F(IndexerTest, LanguageFilters) {
    IndexerConfig config;
    config.languages = {"cpp", "java", "python"};
    
    EXPECT_EQ(config.languages.size(), 3);
    
    bool found_cpp = std::find(config.languages.begin(), config.languages.end(), "cpp") 
                     != config.languages.end();
    bool found_rust = std::find(config.languages.begin(), config.languages.end(), "rust")
                      != config.languages.end();
    
    EXPECT_TRUE(found_cpp);
    EXPECT_FALSE(found_rust);
}

}  // namespace test
}  // namespace aipr
