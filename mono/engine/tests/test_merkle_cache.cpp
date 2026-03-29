/**
 * MerkleCache Tests — Incremental Content-Hash Reindexing
 *
 * Tests:
 * - Schema creation on open()
 * - SHA-256 file hashing (static)
 * - computeDiff identifies new / changed / unchanged / deleted files
 * - updateHashes persists correctly
 * - removeRepo cleans up all entries
 * - getStats returns correct counts
 */

#include <gtest/gtest.h>
#include "merkle_cache.h"
#include <filesystem>
#include <fstream>
#include <unistd.h>

using namespace aipr;

namespace {

class MerkleCacheTest : public ::testing::Test {
protected:
    std::string db_path_;
    std::string repo_dir_;

    void SetUp() override {
        std::string suffix = std::to_string(::getpid());
        db_path_ = "/tmp/aipr_merkle_test_" + suffix + ".db";
        repo_dir_ = "/tmp/aipr_merkle_repo_" + suffix;

        // Clean up any leftovers
        std::filesystem::remove(db_path_);
        std::filesystem::remove(db_path_ + "-wal");
        std::filesystem::remove(db_path_ + "-shm");
        std::filesystem::remove_all(repo_dir_);

        // Create a minimal repo directory with a few test files
        std::filesystem::create_directories(repo_dir_ + "/src");
        writeFile(repo_dir_ + "/src/main.cpp", "int main() { return 0; }");
        writeFile(repo_dir_ + "/src/util.cpp", "void util() {}");
        writeFile(repo_dir_ + "/README.md", "# Test");
    }

    void TearDown() override {
        std::filesystem::remove(db_path_);
        std::filesystem::remove(db_path_ + "-wal");
        std::filesystem::remove(db_path_ + "-shm");
        std::filesystem::remove_all(repo_dir_);
    }

    void writeFile(const std::string& path, const std::string& content) {
        std::ofstream ofs(path);
        ofs << content;
        ofs.close();
    }
};

// ── Open / Close lifecycle ─────────────────────────────────────────────────

TEST_F(MerkleCacheTest, OpenAndCloseSuccessfully) {
    MerkleCacheConfig cfg;
    MerkleCache cache(db_path_, cfg);
    EXPECT_NO_THROW(cache.open());
    EXPECT_NO_THROW(cache.close());
}

TEST_F(MerkleCacheTest, DoubleOpenIsIdempotent) {
    MerkleCache cache(db_path_);
    cache.open();
    EXPECT_NO_THROW(cache.open());
    cache.close();
}

// ── hashFile static method ─────────────────────────────────────────────────

TEST_F(MerkleCacheTest, HashFileReturnsDeterministicSHA256) {
    std::string path = repo_dir_ + "/src/main.cpp";

    std::string h1 = MerkleCache::hashFile(path);
    std::string h2 = MerkleCache::hashFile(path);

    // Must be a hex string of 64 chars (SHA-256)
    EXPECT_EQ(h1.size(), 64u);
    EXPECT_EQ(h1, h2);
}

TEST_F(MerkleCacheTest, HashFileChangesWhenContentChanges) {
    std::string path = repo_dir_ + "/src/main.cpp";
    std::string h1 = MerkleCache::hashFile(path);

    writeFile(path, "int main() { return 42; }");
    std::string h2 = MerkleCache::hashFile(path);

    EXPECT_NE(h1, h2);
}

TEST_F(MerkleCacheTest, HashFileNonexistentReturnsEmpty) {
    std::string h = MerkleCache::hashFile("/tmp/does_not_exist_aipr_xyz.cpp");
    EXPECT_TRUE(h.empty());
}

// ── computeDiff — first run (all new) ──────────────────────────────────────

TEST_F(MerkleCacheTest, ComputeDiffAllNewOnFirstRun) {
    MerkleCache cache(db_path_);
    cache.open();

    std::vector<std::string> all_files = {
        "src/main.cpp", "src/util.cpp", "README.md"
    };

    auto diff = cache.computeDiff("repo1", repo_dir_, all_files, nullptr);

    // All files should be 'new'
    EXPECT_EQ(diff.new_files.size(), 3u);
    EXPECT_EQ(diff.changed_files.size(), 0u);
    EXPECT_EQ(diff.unchanged_files.size(), 0u);
    EXPECT_EQ(diff.deleted_files.size(), 0u);

    EXPECT_EQ(diff.totalToEmbed(), 3u);
    EXPECT_EQ(diff.totalSkipped(), 0u);

    cache.close();
}

// ── computeDiff — second run with no changes (all unchanged) ───────────────

TEST_F(MerkleCacheTest, ComputeDiffAllUnchangedOnSecondRun) {
    MerkleCache cache(db_path_);
    cache.open();

    std::vector<std::string> all_files = {
        "src/main.cpp", "src/util.cpp", "README.md"
    };

    // First run: all new → update hashes
    auto diff1 = cache.computeDiff("repo1", repo_dir_, all_files, nullptr);
    ASSERT_EQ(diff1.new_files.size(), 3u);

    // Persist the hashes
    std::unordered_map<std::string, std::string> hashes;
    for (const auto& f : all_files) {
        hashes[f] = MerkleCache::hashFile(repo_dir_ + "/" + f);
    }
    cache.updateHashes("repo1", hashes);

    // Second run: nothing changed → all unchanged
    auto diff2 = cache.computeDiff("repo1", repo_dir_, all_files, nullptr);
    EXPECT_EQ(diff2.new_files.size(), 0u);
    EXPECT_EQ(diff2.changed_files.size(), 0u);
    EXPECT_EQ(diff2.unchanged_files.size(), 3u);
    EXPECT_EQ(diff2.deleted_files.size(), 0u);
    EXPECT_EQ(diff2.totalSkipped(), 3u);

    cache.close();
}

// ── computeDiff — detect changed file ──────────────────────────────────────

TEST_F(MerkleCacheTest, ComputeDiffDetectsChangedFile) {
    MerkleCache cache(db_path_);
    cache.open();

    std::vector<std::string> all_files = {
        "src/main.cpp", "src/util.cpp"
    };

    // First run: persist hashes
    cache.computeDiff("repo1", repo_dir_, all_files, nullptr);
    std::unordered_map<std::string, std::string> hashes;
    for (const auto& f : all_files) {
        hashes[f] = MerkleCache::hashFile(repo_dir_ + "/" + f);
    }
    cache.updateHashes("repo1", hashes);

    // Modify one file
    writeFile(repo_dir_ + "/src/main.cpp", "int main() { return 999; }");

    // Second run: should detect the change
    auto diff = cache.computeDiff("repo1", repo_dir_, all_files, nullptr);
    EXPECT_EQ(diff.changed_files.size(), 1u);
    EXPECT_EQ(diff.unchanged_files.size(), 1u);
    EXPECT_EQ(diff.new_files.size(), 0u);
    EXPECT_EQ(diff.deleted_files.size(), 0u);

    // The changed file should be main.cpp
    EXPECT_EQ(diff.changed_files[0], "src/main.cpp");

    cache.close();
}

// ── computeDiff — detect deleted file ──────────────────────────────────────

TEST_F(MerkleCacheTest, ComputeDiffDetectsDeletedFile) {
    MerkleCache cache(db_path_);
    cache.open();

    std::vector<std::string> all_files = {
        "src/main.cpp", "src/util.cpp"
    };

    // First run: persist hashes
    cache.computeDiff("repo1", repo_dir_, all_files, nullptr);
    std::unordered_map<std::string, std::string> hashes;
    for (const auto& f : all_files) {
        hashes[f] = MerkleCache::hashFile(repo_dir_ + "/" + f);
    }
    cache.updateHashes("repo1", hashes);

    // Second run: omit util.cpp from the file list (simulating deletion)
    std::vector<std::string> reduced = {"src/main.cpp"};
    auto diff = cache.computeDiff("repo1", repo_dir_, reduced, nullptr);

    EXPECT_EQ(diff.deleted_files.size(), 1u);
    EXPECT_EQ(diff.deleted_files[0], "src/util.cpp");
    EXPECT_EQ(diff.unchanged_files.size(), 1u);

    cache.close();
}

// ── computeDiff — detect new file ──────────────────────────────────────────

TEST_F(MerkleCacheTest, ComputeDiffDetectsNewFile) {
    MerkleCache cache(db_path_);
    cache.open();

    std::vector<std::string> initial = {"src/main.cpp"};

    // Persist initial
    cache.computeDiff("repo1", repo_dir_, initial, nullptr);
    std::unordered_map<std::string, std::string> hashes;
    hashes["src/main.cpp"] = MerkleCache::hashFile(repo_dir_ + "/src/main.cpp");
    cache.updateHashes("repo1", hashes);

    // Add new file to list (file already exists on disk from SetUp)
    std::vector<std::string> expanded = {"src/main.cpp", "src/util.cpp"};
    auto diff = cache.computeDiff("repo1", repo_dir_, expanded, nullptr);

    EXPECT_EQ(diff.new_files.size(), 1u);
    EXPECT_EQ(diff.new_files[0], "src/util.cpp");
    EXPECT_EQ(diff.unchanged_files.size(), 1u);

    cache.close();
}

// ── removeRepo cleans all entries ──────────────────────────────────────────

TEST_F(MerkleCacheTest, RemoveRepoCleansEntries) {
    MerkleCache cache(db_path_);
    cache.open();

    std::vector<std::string> files = {"src/main.cpp"};
    cache.computeDiff("repo1", repo_dir_, files, nullptr);
    std::unordered_map<std::string, std::string> hashes;
    hashes["src/main.cpp"] = MerkleCache::hashFile(repo_dir_ + "/src/main.cpp");
    cache.updateHashes("repo1", hashes);

    // Verify the hash is stored
    auto h = cache.getHash("repo1", "src/main.cpp");
    EXPECT_FALSE(h.empty());

    // Remove the repo
    cache.removeRepo("repo1");

    // Hash should be gone now
    auto h2 = cache.getHash("repo1", "src/main.cpp");
    EXPECT_TRUE(h2.empty());

    // computeDiff should show all as new again
    auto diff = cache.computeDiff("repo1", repo_dir_, files, nullptr);
    EXPECT_EQ(diff.new_files.size(), 1u);
    EXPECT_EQ(diff.unchanged_files.size(), 0u);

    cache.close();
}

// ── getStats returns correct counts ────────────────────────────────────────

TEST_F(MerkleCacheTest, GetStatsReturnsCorrectCounts) {
    MerkleCache cache(db_path_);
    cache.open();

    std::vector<std::string> files = {"src/main.cpp", "src/util.cpp"};
    cache.computeDiff("repo1", repo_dir_, files, nullptr);

    std::unordered_map<std::string, std::string> hashes;
    for (const auto& f : files) {
        hashes[f] = MerkleCache::hashFile(repo_dir_ + "/" + f);
    }
    cache.updateHashes("repo1", hashes);

    auto stats = cache.getStats("repo1");
    EXPECT_EQ(stats.total_entries, 2u);

    cache.close();
}

// ── MerkleDiffResult helper methods ────────────────────────────────────────

TEST_F(MerkleCacheTest, DiffResultHelperMethods) {
    MerkleDiffResult diff;
    diff.new_files = {"a.cpp", "b.cpp"};
    diff.changed_files = {"c.cpp"};
    diff.unchanged_files = {"d.cpp", "e.cpp"};
    diff.deleted_files = {"f.cpp"};

    // totalToEmbed = new + changed + dependent
    EXPECT_EQ(diff.totalToEmbed(), 3u);

    // totalSkipped = unchanged
    EXPECT_EQ(diff.totalSkipped(), 2u);
}

// ── Multiple repos do not interfere ────────────────────────────────────────

TEST_F(MerkleCacheTest, MultipleReposAreIsolated) {
    MerkleCache cache(db_path_);
    cache.open();

    std::vector<std::string> files = {"src/main.cpp"};

    // Repo 1
    cache.computeDiff("repo1", repo_dir_, files, nullptr);
    std::unordered_map<std::string, std::string> h1;
    h1["src/main.cpp"] = MerkleCache::hashFile(repo_dir_ + "/src/main.cpp");
    cache.updateHashes("repo1", h1);

    // Repo 2
    cache.computeDiff("repo2", repo_dir_, files, nullptr);
    cache.updateHashes("repo2", h1);

    // Removing repo1 should not affect repo2
    cache.removeRepo("repo1");

    auto h_r1 = cache.getHash("repo1", "src/main.cpp");
    auto h_r2 = cache.getHash("repo2", "src/main.cpp");
    EXPECT_TRUE(h_r1.empty());
    EXPECT_FALSE(h_r2.empty());

    cache.close();
}

} // namespace
