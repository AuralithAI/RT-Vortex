/**
 * AI PR Reviewer - Performance Timer
 * 
 * High-resolution timing for profiling and metrics.
 */

#include <string>
#include <chrono>
#include <unordered_map>
#include <vector>
#include <mutex>
#include <iomanip>
#include <sstream>
#include <algorithm>
#include <numeric>

namespace aipr {
namespace perf {

using Clock = std::chrono::high_resolution_clock;
using TimePoint = Clock::time_point;
using Duration = std::chrono::nanoseconds;

/**
 * Simple scoped timer
 */
class ScopedTimer {
public:
    ScopedTimer() : start_(Clock::now()) {}
    
    ~ScopedTimer() = default;
    
    double elapsedMs() const {
        auto now = Clock::now();
        return std::chrono::duration<double, std::milli>(now - start_).count();
    }
    
    double elapsedUs() const {
        auto now = Clock::now();
        return std::chrono::duration<double, std::micro>(now - start_).count();
    }
    
    double elapsedNs() const {
        auto now = Clock::now();
        return std::chrono::duration<double, std::nano>(now - start_).count();
    }
    
    void reset() {
        start_ = Clock::now();
    }
    
private:
    TimePoint start_;
};

/**
 * Timing statistics
 */
struct TimingStats {
    size_t count = 0;
    double total_ms = 0.0;
    double min_ms = std::numeric_limits<double>::max();
    double max_ms = 0.0;
    double avg_ms = 0.0;
    double p50_ms = 0.0;
    double p95_ms = 0.0;
    double p99_ms = 0.0;
    
    std::string toString() const {
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
};

/**
 * Profiler for collecting timing metrics
 */
class Profiler {
public:
    static Profiler& instance() {
        static Profiler instance;
        return instance;
    }
    
    /**
     * Record a timing sample
     */
    void record(const std::string& name, double ms) {
        std::lock_guard<std::mutex> lock(mutex_);
        timings_[name].push_back(ms);
    }
    
    /**
     * Start a named timer
     */
    void start(const std::string& name) {
        std::lock_guard<std::mutex> lock(mutex_);
        active_timers_[name] = Clock::now();
    }
    
    /**
     * Stop a named timer and record
     */
    void stop(const std::string& name) {
        TimePoint now = Clock::now();
        
        std::lock_guard<std::mutex> lock(mutex_);
        auto it = active_timers_.find(name);
        if (it != active_timers_.end()) {
            double ms = std::chrono::duration<double, std::milli>(now - it->second).count();
            timings_[name].push_back(ms);
            active_timers_.erase(it);
        }
    }
    
    /**
     * Get statistics for a metric
     */
    TimingStats getStats(const std::string& name) const {
        std::lock_guard<std::mutex> lock(mutex_);
        
        TimingStats stats;
        auto it = timings_.find(name);
        if (it == timings_.end() || it->second.empty()) {
            stats.min_ms = 0.0;
            return stats;
        }
        
        auto samples = it->second;  // Copy for sorting
        stats.count = samples.size();
        stats.total_ms = std::accumulate(samples.begin(), samples.end(), 0.0);
        stats.min_ms = *std::min_element(samples.begin(), samples.end());
        stats.max_ms = *std::max_element(samples.begin(), samples.end());
        stats.avg_ms = stats.total_ms / stats.count;
        
        // Percentiles
        std::sort(samples.begin(), samples.end());
        stats.p50_ms = percentile(samples, 50);
        stats.p95_ms = percentile(samples, 95);
        stats.p99_ms = percentile(samples, 99);
        
        return stats;
    }
    
    /**
     * Get all metric names
     */
    std::vector<std::string> getMetricNames() const {
        std::lock_guard<std::mutex> lock(mutex_);
        
        std::vector<std::string> names;
        for (const auto& [name, _] : timings_) {
            names.push_back(name);
        }
        return names;
    }
    
    /**
     * Clear all timings
     */
    void clear() {
        std::lock_guard<std::mutex> lock(mutex_);
        timings_.clear();
        active_timers_.clear();
    }
    
    /**
     * Generate report
     */
    std::string report() const {
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
    
private:
    Profiler() = default;
    
    double percentile(const std::vector<double>& sorted, int p) const {
        if (sorted.empty()) return 0.0;
        size_t idx = (sorted.size() * p) / 100;
        if (idx >= sorted.size()) idx = sorted.size() - 1;
        return sorted[idx];
    }
    
    mutable std::mutex mutex_;
    std::unordered_map<std::string, std::vector<double>> timings_;
    std::unordered_map<std::string, TimePoint> active_timers_;
};

/**
 * RAII timer that records to profiler
 */
class AutoTimer {
public:
    explicit AutoTimer(const std::string& name) : name_(name) {}
    
    ~AutoTimer() {
        double ms = timer_.elapsedMs();
        Profiler::instance().record(name_, ms);
    }
    
    // No copy
    AutoTimer(const AutoTimer&) = delete;
    AutoTimer& operator=(const AutoTimer&) = delete;
    
private:
    std::string name_;
    ScopedTimer timer_;
};

// Convenience macros
#define AIPR_PROFILE_SCOPE(name) aipr::perf::AutoTimer __timer_##__LINE__(name)
#define AIPR_PROFILE_FUNCTION() aipr::perf::AutoTimer __timer_##__LINE__(__func__)

/**
 * Rate limiter
 */
class RateLimiter {
public:
    RateLimiter(size_t max_per_second) 
        : max_per_second_(max_per_second)
        , tokens_(max_per_second)
        , last_update_(Clock::now()) {}
    
    bool tryAcquire() {
        std::lock_guard<std::mutex> lock(mutex_);
        refill();
        
        if (tokens_ > 0) {
            tokens_--;
            return true;
        }
        return false;
    }
    
    void waitAndAcquire() {
        while (!tryAcquire()) {
            std::this_thread::sleep_for(std::chrono::milliseconds(1));
        }
    }
    
private:
    void refill() {
        auto now = Clock::now();
        double elapsed_sec = std::chrono::duration<double>(now - last_update_).count();
        
        size_t new_tokens = static_cast<size_t>(elapsed_sec * max_per_second_);
        if (new_tokens > 0) {
            tokens_ = std::min(tokens_ + new_tokens, max_per_second_);
            last_update_ = now;
        }
    }
    
    size_t max_per_second_;
    size_t tokens_;
    TimePoint last_update_;
    std::mutex mutex_;
};

} // namespace perf
} // namespace aipr
