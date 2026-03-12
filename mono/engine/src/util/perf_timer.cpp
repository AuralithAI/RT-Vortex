/**
 * AI PR Reviewer — Performance Timer Implementation
 *
 * High-resolution timing for profiling and metrics.
 * See perf_timer.h for class declarations.
 */

#include "perf_timer.h"

#include <algorithm>
#include <iomanip>
#include <numeric>
#include <sstream>
#include <thread>

namespace aipr {
namespace perf {

// ── TimingStats ─────────────────────────────────────────────────────────────

std::string TimingStats::toString() const {
    std::ostringstream ss;
    ss << std::fixed << std::setprecision(3);
    ss << "count=" << count
       << " total=" << total_ms << "ms"
       << " avg=" << avg_ms << "ms"
       << " min=" << min_ms << "ms"
       << " max=" << max_ms << "ms"
       << " p50=" << p50_ms << "ms"
       << " p95=" << p95_ms << "ms"
       << " p99=" << p99_ms << "ms";
    return ss.str();
}

// ── Profiler ────────────────────────────────────────────────────────────────

Profiler& Profiler::instance() {
    static Profiler inst;
    return inst;
}

void Profiler::record(const std::string& name, double ms) {
    std::lock_guard<std::mutex> lock(mutex_);
    timings_[name].push_back(ms);
}

void Profiler::start(const std::string& name) {
    std::lock_guard<std::mutex> lock(mutex_);
    active_timers_[name] = Clock::now();
}

void Profiler::stop(const std::string& name) {
    TimePoint now = Clock::now();

    std::lock_guard<std::mutex> lock(mutex_);
    auto it = active_timers_.find(name);
    if (it != active_timers_.end()) {
        double ms = std::chrono::duration<double, std::milli>(now - it->second).count();
        timings_[name].push_back(ms);
        active_timers_.erase(it);
    }
}

TimingStats Profiler::getStats(const std::string& name) const {
    std::lock_guard<std::mutex> lock(mutex_);

    TimingStats stats;
    auto it = timings_.find(name);
    if (it == timings_.end() || it->second.empty()) {
        stats.min_ms = 0.0;
        return stats;
    }

    auto samples = it->second;  // copy for sorting
    stats.count    = samples.size();
    stats.total_ms = std::accumulate(samples.begin(), samples.end(), 0.0);
    stats.min_ms   = *std::min_element(samples.begin(), samples.end());
    stats.max_ms   = *std::max_element(samples.begin(), samples.end());
    stats.avg_ms   = stats.total_ms / static_cast<double>(stats.count);

    std::sort(samples.begin(), samples.end());
    stats.p50_ms = percentile(samples, 50);
    stats.p95_ms = percentile(samples, 95);
    stats.p99_ms = percentile(samples, 99);

    return stats;
}

std::vector<std::string> Profiler::getMetricNames() const {
    std::lock_guard<std::mutex> lock(mutex_);

    std::vector<std::string> names;
    names.reserve(timings_.size());
    for (const auto& [name, _] : timings_) {
        names.push_back(name);
    }
    return names;
}

void Profiler::clear() {
    std::lock_guard<std::mutex> lock(mutex_);
    timings_.clear();
    active_timers_.clear();
}

std::string Profiler::report() const {
    std::ostringstream ss;
    ss << "=== Performance Report ===\n";

    auto names = getMetricNames();
    std::sort(names.begin(), names.end());

    for (const auto& name : names) {
        auto stats = getStats(name);
        ss << name << ": " << stats.toString() << "\n";
    }

    return ss.str();
}

double Profiler::percentile(const std::vector<double>& sorted, int p) const {
    if (sorted.empty()) return 0.0;
    size_t idx = (sorted.size() * static_cast<size_t>(p)) / 100;
    if (idx >= sorted.size()) idx = sorted.size() - 1;
    return sorted[idx];
}

// ── RateLimiter ─────────────────────────────────────────────────────────────

RateLimiter::RateLimiter(size_t max_per_second)
    : max_per_second_(max_per_second)
    , tokens_(max_per_second)
    , last_update_(Clock::now()) {}

bool RateLimiter::tryAcquire() {
    std::lock_guard<std::mutex> lock(mutex_);
    refill();

    if (tokens_ > 0) {
        tokens_--;
        return true;
    }
    return false;
}

void RateLimiter::waitAndAcquire() {
    while (!tryAcquire()) {
        std::this_thread::sleep_for(std::chrono::milliseconds(1));
    }
}

void RateLimiter::refill() {
    auto now = Clock::now();
    double elapsed_sec = std::chrono::duration<double>(now - last_update_).count();

    auto new_tokens = static_cast<size_t>(elapsed_sec * static_cast<double>(max_per_second_));
    if (new_tokens > 0) {
        tokens_ = std::min(tokens_ + new_tokens, max_per_second_);
        last_update_ = now;
    }
}

} // namespace perf
} // namespace aipr
