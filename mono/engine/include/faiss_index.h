/**
 * AI PR Reviewer - FAISS Index Wrapper (Internal)
 *
 * Shared by LongTermMemory and MetaTaskMemory.
 */
#pragma once

#include <vector>
#include <string>
#include <stdexcept>

#ifdef AIPR_HAS_FAISS
#include <faiss/IndexFlat.h>
#include <faiss/IndexIDMap.h>
#include <faiss/index_io.h>
#include <faiss/impl/IDSelector.h>
#endif

namespace aipr {

class FAISSIndex {
public:
    explicit FAISSIndex(size_t dimension) : dimension_(dimension) {
#ifdef AIPR_HAS_FAISS
        auto flat_index = new faiss::IndexFlatL2(dimension);
        index_ = new faiss::IndexIDMap(flat_index);
#endif
    }

    ~FAISSIndex() {
#ifdef AIPR_HAS_FAISS
        delete index_;
#endif
    }

    void add(int64_t id, const std::vector<float>& embedding) {
#ifdef AIPR_HAS_FAISS
        if (embedding.size() != dimension_) {
            throw std::invalid_argument("Embedding dimension mismatch");
        }
        index_->add_with_ids(1, embedding.data(), &id);
#endif
    }

    void addBatch(const std::vector<int64_t>& ids, const std::vector<float>& embeddings) {
#ifdef AIPR_HAS_FAISS
        if (embeddings.size() != ids.size() * dimension_) {
            throw std::invalid_argument("Embedding count mismatch");
        }
        index_->add_with_ids(ids.size(), embeddings.data(), ids.data());
#endif
    }

    std::vector<std::pair<int64_t, float>> search(
        const std::vector<float>& query,
        int top_k
    ) {
        std::vector<std::pair<int64_t, float>> results;

#ifdef AIPR_HAS_FAISS
        std::vector<float> distances(top_k);
        std::vector<faiss::idx_t> labels(top_k);

        index_->search(1, query.data(), top_k, distances.data(), labels.data());

        for (int i = 0; i < top_k; ++i) {
            if (labels[i] >= 0) {
                results.emplace_back(labels[i], distances[i]);
            }
        }
#else
        (void)query;
        (void)top_k;
#endif

        return results;
    }

    void remove(int64_t id) {
#ifdef AIPR_HAS_FAISS
        faiss::IDSelectorArray selector(1, &id);
        index_->remove_ids(selector);
#else
        (void)id;
#endif
    }

    size_t size() const {
#ifdef AIPR_HAS_FAISS
        return index_->ntotal;
#else
        return 0;
#endif
    }

    void save(const std::string& path) {
#ifdef AIPR_HAS_FAISS
        faiss::write_index(index_, path.c_str());
#else
        (void)path;
#endif
    }

    void load(const std::string& path) {
#ifdef AIPR_HAS_FAISS
        delete index_;
        index_ = faiss::read_index(path.c_str());
#else
        (void)path;
#endif
    }

private:
    size_t dimension_;
#ifdef AIPR_HAS_FAISS
    faiss::Index* index_ = nullptr;
#endif
};

} // namespace aipr
