/**
 * MultiVectorIndex Tests — Matryoshka Dual-Resolution FAISS Search
 *
 * Tests:
 * - Config defaults
 * - Single-resolution mode (enabled=false) acts as passthrough
 * - Dual-resolution: add, search, hybridSearch
 * - Stats reflect both indexes
 * - Truncation correctness
 * - Save / Load round-trip
 * - Remove and removeByRepo
 */

#include <gtest/gtest.h>
#include "tms/multi_vector_index.h"
#include <filesystem>
#include <cmath>

using namespace aipr::tms;

namespace {

// ── Helpers ────────────────────────────────────────────────────────────────

static std::vector<float> makeEmbedding(size_t dim, float seed) {
    std::vector<float> v(dim);
    for (size_t i = 0; i < dim; ++i) {
        v[i] = seed + static_cast<float>(i) * 0.01f;
    }
    // Normalize for cosine similarity
    float norm = 0;
    for (auto x : v) norm += x * x;
    norm = std::sqrt(norm);
    if (norm > 0) for (auto& x : v) x /= norm;
    return v;
}

static CodeChunk makeChunk(const std::string& id,
                           const std::string& file_path,
                           const std::string& content) {
    CodeChunk c;
    c.id = id;
    c.file_path = file_path;
    c.content = content;
    c.type = "function";
    c.language = "cpp";
    c.name = id;
    return c;
}

// ── Test Fixture ───────────────────────────────────────────────────────────

class MultiVectorIndexTest : public ::testing::Test {
protected:
    std::string storage_;

    // Use small dimensions for fast tests
    static constexpr size_t kFineDim   = 32;
    static constexpr size_t kCoarseDim = 8;

    void SetUp() override {
        storage_ = "/tmp/aipr_multivec_test_" + std::to_string(::getpid());
        std::filesystem::create_directories(storage_);
    }

    void TearDown() override {
        std::filesystem::remove_all(storage_);
    }

    MultiVectorConfig dualConfig() {
        MultiVectorConfig cfg;
        cfg.fine_dimension = kFineDim;
        cfg.coarse_dimension = kCoarseDim;
        cfg.oversampling_factor = 3;
        cfg.enabled = true;
        cfg.storage_path = storage_;
        return cfg;
    }

    MultiVectorConfig singleConfig() {
        MultiVectorConfig cfg;
        cfg.fine_dimension = kFineDim;
        cfg.coarse_dimension = kCoarseDim;
        cfg.enabled = false;      // single-resolution mode
        cfg.storage_path = storage_;
        return cfg;
    }

    LTMConfig baseLTMConfig(size_t dim) {
        LTMConfig lc;
        lc.dimension = dim;
        lc.storage_path = storage_;
        lc.use_cosine_similarity = true;
        lc.index_type = FAISSIndexType::FLAT_L2;
        return lc;
    }
};

// ── Config Defaults ────────────────────────────────────────────────────────

TEST_F(MultiVectorIndexTest, ConfigDefaults) {
    MultiVectorConfig cfg;
    EXPECT_EQ(cfg.fine_dimension, 1024u);
    EXPECT_EQ(cfg.coarse_dimension, 384u);
    EXPECT_EQ(cfg.oversampling_factor, 3);
    EXPECT_FALSE(cfg.enabled);
}

// ── Single-resolution mode (passthrough) ───────────────────────────────────

TEST_F(MultiVectorIndexTest, SingleResolutionModeNotDualActive) {
    auto ltm_cfg = baseLTMConfig(kFineDim);
    MultiVectorIndex idx(singleConfig(), ltm_cfg);

    EXPECT_FALSE(idx.isDualActive());

    auto stats = idx.getStats();
    EXPECT_FALSE(stats.dual_index_active);
}

TEST_F(MultiVectorIndexTest, SingleResolutionAddAndSearch) {
    auto ltm_cfg = baseLTMConfig(kFineDim);
    MultiVectorIndex idx(singleConfig(), ltm_cfg);

    auto chunk = makeChunk("c1", "src/main.cpp", "int main() {}");
    auto emb = makeEmbedding(kFineDim, 0.1f);
    idx.add(chunk, emb);

    auto results = idx.search(emb, 5);

    // Should find the chunk we just added
    ASSERT_GE(results.size(), 1u);
    EXPECT_EQ(results[0].chunk.id, "c1");
}

// ── Dual-resolution mode ───────────────────────────────────────────────────

TEST_F(MultiVectorIndexTest, DualResolutionIsDualActive) {
    auto ltm_cfg = baseLTMConfig(kFineDim);
    MultiVectorIndex idx(dualConfig(), ltm_cfg);

    EXPECT_TRUE(idx.isDualActive());
}

TEST_F(MultiVectorIndexTest, DualResolutionAddAndSearch) {
    auto ltm_cfg = baseLTMConfig(kFineDim);
    MultiVectorIndex idx(dualConfig(), ltm_cfg);

    // Add several chunks with distinct embeddings
    for (int i = 0; i < 5; ++i) {
        auto chunk = makeChunk("c" + std::to_string(i),
                               "src/f" + std::to_string(i) + ".cpp",
                               "void f" + std::to_string(i) + "() {}");
        auto emb = makeEmbedding(kFineDim, 0.1f * (i + 1));
        idx.add(chunk, emb);
    }

    // Search: the dual-resolution path (coarse screen → fine rerank) should
    // return the requested number of results from the indexed chunks.
    auto query = makeEmbedding(kFineDim, 0.1f);
    auto results = idx.search(query, 3);

    ASSERT_GE(results.size(), 1u);
    EXPECT_LE(results.size(), 3u);

    // All returned chunks should have a valid id from the indexed set
    for (const auto& r : results) {
        EXPECT_FALSE(r.chunk.id.empty());
        EXPECT_TRUE(r.chunk.id.find("c") == 0)
            << "unexpected chunk id: " << r.chunk.id;
    }
}

TEST_F(MultiVectorIndexTest, DualResolutionBatchAdd) {
    auto ltm_cfg = baseLTMConfig(kFineDim);
    MultiVectorIndex idx(dualConfig(), ltm_cfg);

    std::vector<CodeChunk> chunks;
    std::vector<std::vector<float>> embeddings;
    for (int i = 0; i < 10; ++i) {
        chunks.push_back(makeChunk("batch" + std::to_string(i),
                                   "src/b" + std::to_string(i) + ".cpp",
                                   "code"));
        embeddings.push_back(makeEmbedding(kFineDim, 0.05f * (i + 1)));
    }

    idx.addBatch(chunks, embeddings);

    auto stats = idx.getStats();
    EXPECT_EQ(stats.total_chunks, 10u);
    EXPECT_EQ(stats.fine_index_vectors, 10u);
    EXPECT_EQ(stats.coarse_index_vectors, 10u);
}

// ── Stats reflect correct dimensions ───────────────────────────────────────

TEST_F(MultiVectorIndexTest, StatsReportCorrectDimensions) {
    auto ltm_cfg = baseLTMConfig(kFineDim);
    MultiVectorIndex idx(dualConfig(), ltm_cfg);

    auto stats = idx.getStats();
    EXPECT_EQ(stats.fine_dimension, kFineDim);
    EXPECT_EQ(stats.coarse_dimension, kCoarseDim);
    EXPECT_TRUE(stats.dual_index_active);
}

// ── Remove operations ──────────────────────────────────────────────────────
// Note: FAISS HNSW indexes don't support remove_ids.  Use FLAT_L2 for these
// tests so removal is possible without a full index rebuild.

TEST_F(MultiVectorIndexTest, RemoveByChunkId) {
    auto ltm_cfg = baseLTMConfig(kFineDim);
    ltm_cfg.index_type = FAISSIndexType::FLAT_L2;   // supports remove_ids

    auto cfg = singleConfig();  // single-resolution avoids HNSW coarse index
    MultiVectorIndex idx(cfg, ltm_cfg);

    auto c1 = makeChunk("c1", "a.cpp", "code1");
    auto c2 = makeChunk("c2", "b.cpp", "code2");
    idx.add(c1, makeEmbedding(kFineDim, 0.1f));
    idx.add(c2, makeEmbedding(kFineDim, 0.5f));

    EXPECT_TRUE(idx.remove("c1"));

    // c1 should no longer be findable
    auto& fi = idx.fineIndex();
    EXPECT_FALSE(fi.contains("c1"));
    EXPECT_TRUE(fi.contains("c2"));
}

TEST_F(MultiVectorIndexTest, RemoveByRepo) {
    auto ltm_cfg = baseLTMConfig(kFineDim);
    ltm_cfg.index_type = FAISSIndexType::FLAT_L2;   // supports remove_ids

    auto cfg = singleConfig();
    MultiVectorIndex idx(cfg, ltm_cfg);

    // Chunks have repo prefix in id
    CodeChunk c1;
    c1.id = "repo1:src/a.cpp:func";
    c1.file_path = "src/a.cpp";
    c1.type = "function";
    c1.language = "cpp";
    c1.content = "code";

    CodeChunk c2;
    c2.id = "repo2:src/b.cpp:func";
    c2.file_path = "src/b.cpp";
    c2.type = "function";
    c2.language = "cpp";
    c2.content = "code";

    idx.add(c1, makeEmbedding(kFineDim, 0.1f));
    idx.add(c2, makeEmbedding(kFineDim, 0.5f));

    size_t removed = idx.removeByRepo("repo1");
    EXPECT_GE(removed, 1u);
}

// ── Save / Load round-trip ─────────────────────────────────────────────────

TEST_F(MultiVectorIndexTest, SaveAndLoadPreservesData) {
    auto ltm_cfg = baseLTMConfig(kFineDim);

    {
        MultiVectorIndex idx(dualConfig(), ltm_cfg);
        auto chunk = makeChunk("persist1", "p.cpp", "persist me");
        idx.add(chunk, makeEmbedding(kFineDim, 0.3f));
        idx.save();
    }

    // Reload in a new instance
    {
        MultiVectorIndex idx2(dualConfig(), ltm_cfg);
        idx2.load();

        auto query = makeEmbedding(kFineDim, 0.3f);
        auto results = idx2.search(query, 5);

        ASSERT_GE(results.size(), 1u);
        EXPECT_EQ(results[0].chunk.id, "persist1");
    }
}

// ── fineIndex() exposes the underlying LTM ─────────────────────────────────

TEST_F(MultiVectorIndexTest, FineIndexIsAccessible) {
    auto ltm_cfg = baseLTMConfig(kFineDim);
    MultiVectorIndex idx(dualConfig(), ltm_cfg);

    auto chunk = makeChunk("fi1", "f.cpp", "code");
    idx.add(chunk, makeEmbedding(kFineDim, 0.2f));

    // The fine index should also see this chunk
    auto& fi = idx.fineIndex();
    EXPECT_TRUE(fi.contains("fi1"));
}

// ── hybridSearch basic operation ───────────────────────────────────────────

TEST_F(MultiVectorIndexTest, HybridSearchReturnsResults) {
    auto ltm_cfg = baseLTMConfig(kFineDim);
    MultiVectorIndex idx(dualConfig(), ltm_cfg);

    auto chunk = makeChunk("hs1", "search.cpp", "void search() { find(); }");
    idx.add(chunk, makeEmbedding(kFineDim, 0.15f));

    auto query = makeEmbedding(kFineDim, 0.15f);
    auto results = idx.hybridSearch("search find", query, 5);

    ASSERT_GE(results.size(), 1u);
}

} // namespace
