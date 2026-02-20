/**
 * AI PR Reviewer - Hashing Utilities
 * 
 * Content hashing for change detection and deduplication.
 */

#include <string>
#include <array>
#include <cstdint>
#include <cstring>
#include <sstream>
#include <iomanip>

namespace aipr {
namespace hash {

/**
 * Simple xxHash-like fast hash for content fingerprinting
 */
class XXHash64 {
public:
    static constexpr uint64_t PRIME1 = 11400714785074694791ULL;
    static constexpr uint64_t PRIME2 = 14029467366897019727ULL;
    static constexpr uint64_t PRIME3 = 1609587929392839161ULL;
    static constexpr uint64_t PRIME4 = 9650029242287828579ULL;
    static constexpr uint64_t PRIME5 = 2870177450012600261ULL;
    
    explicit XXHash64(uint64_t seed = 0) : seed_(seed) {}
    
    uint64_t hash(const void* data, size_t len) const {
        const uint8_t* ptr = static_cast<const uint8_t*>(data);
        const uint8_t* end = ptr + len;
        uint64_t h64;
        
        if (len >= 32) {
            const uint8_t* limit = end - 32;
            uint64_t v1 = seed_ + PRIME1 + PRIME2;
            uint64_t v2 = seed_ + PRIME2;
            uint64_t v3 = seed_;
            uint64_t v4 = seed_ - PRIME1;
            
            do {
                v1 = round(v1, read64(ptr)); ptr += 8;
                v2 = round(v2, read64(ptr)); ptr += 8;
                v3 = round(v3, read64(ptr)); ptr += 8;
                v4 = round(v4, read64(ptr)); ptr += 8;
            } while (ptr <= limit);
            
            h64 = rotl(v1, 1) + rotl(v2, 7) + rotl(v3, 12) + rotl(v4, 18);
            h64 = mergeRound(h64, v1);
            h64 = mergeRound(h64, v2);
            h64 = mergeRound(h64, v3);
            h64 = mergeRound(h64, v4);
        } else {
            h64 = seed_ + PRIME5;
        }
        
        h64 += static_cast<uint64_t>(len);
        
        while (ptr + 8 <= end) {
            h64 ^= round(0, read64(ptr));
            h64 = rotl(h64, 27) * PRIME1 + PRIME4;
            ptr += 8;
        }
        
        if (ptr + 4 <= end) {
            h64 ^= static_cast<uint64_t>(read32(ptr)) * PRIME1;
            h64 = rotl(h64, 23) * PRIME2 + PRIME3;
            ptr += 4;
        }
        
        while (ptr < end) {
            h64 ^= static_cast<uint64_t>(*ptr) * PRIME5;
            h64 = rotl(h64, 11) * PRIME1;
            ptr++;
        }
        
        return finalize(h64);
    }
    
    uint64_t hash(const std::string& str) const {
        return hash(str.data(), str.size());
    }
    
    std::string hashHex(const std::string& str) const {
        uint64_t h = hash(str);
        std::ostringstream ss;
        ss << std::hex << std::setfill('0') << std::setw(16) << h;
        return ss.str();
    }
    
private:
    uint64_t seed_;
    
    static uint64_t read64(const uint8_t* ptr) {
        uint64_t val = 0;
        for (int i = 0; i < 8; i++) {
            val |= static_cast<uint64_t>(ptr[i]) << (i * 8);
        }
        return val;
    }
    
    static uint32_t read32(const uint8_t* ptr) {
        uint32_t val = 0;
        for (int i = 0; i < 4; i++) {
            val |= static_cast<uint32_t>(ptr[i]) << (i * 8);
        }
        return val;
    }
    
    static uint64_t rotl(uint64_t x, int r) {
        return (x << r) | (x >> (64 - r));
    }
    
    static uint64_t round(uint64_t acc, uint64_t input) {
        acc += input * PRIME2;
        acc = rotl(acc, 31);
        acc *= PRIME1;
        return acc;
    }
    
    static uint64_t mergeRound(uint64_t acc, uint64_t val) {
        val = round(0, val);
        acc ^= val;
        acc = acc * PRIME1 + PRIME4;
        return acc;
    }
    
    static uint64_t finalize(uint64_t h64) {
        h64 ^= h64 >> 33;
        h64 *= PRIME2;
        h64 ^= h64 >> 29;
        h64 *= PRIME3;
        h64 ^= h64 >> 32;
        return h64;
    }
};

/**
 * Simple SHA-256 implementation for content hashing
 */
class SHA256 {
public:
    std::array<uint8_t, 32> hash(const void* data, size_t len) {
        init();
        update(static_cast<const uint8_t*>(data), len);
        return final();
    }
    
    std::string hashHex(const std::string& str) {
        auto digest = hash(str.data(), str.size());
        std::ostringstream ss;
        for (uint8_t byte : digest) {
            ss << std::hex << std::setfill('0') << std::setw(2) << static_cast<int>(byte);
        }
        return ss.str();
    }
    
private:
    uint32_t state_[8];
    uint8_t buffer_[64];
    size_t buffer_len_;
    uint64_t total_len_;
    
    static constexpr uint32_t K[64] = {
        0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
        0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
        0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
        0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
        0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
        0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
        0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
        0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
    };
    
    void init() {
        state_[0] = 0x6a09e667;
        state_[1] = 0xbb67ae85;
        state_[2] = 0x3c6ef372;
        state_[3] = 0xa54ff53a;
        state_[4] = 0x510e527f;
        state_[5] = 0x9b05688c;
        state_[6] = 0x1f83d9ab;
        state_[7] = 0x5be0cd19;
        buffer_len_ = 0;
        total_len_ = 0;
    }
    
    void update(const uint8_t* data, size_t len) {
        total_len_ += len;
        
        if (buffer_len_ > 0) {
            size_t need = 64 - buffer_len_;
            if (len < need) {
                memcpy(buffer_ + buffer_len_, data, len);
                buffer_len_ += len;
                return;
            }
            memcpy(buffer_ + buffer_len_, data, need);
            transform(buffer_);
            data += need;
            len -= need;
            buffer_len_ = 0;
        }
        
        while (len >= 64) {
            transform(data);
            data += 64;
            len -= 64;
        }
        
        if (len > 0) {
            memcpy(buffer_, data, len);
            buffer_len_ = len;
        }
    }
    
    std::array<uint8_t, 32> final() {
        // Padding
        buffer_[buffer_len_++] = 0x80;
        
        if (buffer_len_ > 56) {
            while (buffer_len_ < 64) {
                buffer_[buffer_len_++] = 0;
            }
            transform(buffer_);
            buffer_len_ = 0;
        }
        
        while (buffer_len_ < 56) {
            buffer_[buffer_len_++] = 0;
        }
        
        // Length in bits
        uint64_t bits = total_len_ * 8;
        for (int i = 7; i >= 0; i--) {
            buffer_[buffer_len_++] = static_cast<uint8_t>(bits >> (i * 8));
        }
        transform(buffer_);
        
        // Output
        std::array<uint8_t, 32> result;
        for (int i = 0; i < 8; i++) {
            result[i * 4 + 0] = static_cast<uint8_t>(state_[i] >> 24);
            result[i * 4 + 1] = static_cast<uint8_t>(state_[i] >> 16);
            result[i * 4 + 2] = static_cast<uint8_t>(state_[i] >> 8);
            result[i * 4 + 3] = static_cast<uint8_t>(state_[i]);
        }
        return result;
    }
    
    static uint32_t rotr(uint32_t x, int n) {
        return (x >> n) | (x << (32 - n));
    }
    
    void transform(const uint8_t* block) {
        uint32_t w[64];
        
        for (int i = 0; i < 16; i++) {
            w[i] = (static_cast<uint32_t>(block[i * 4]) << 24) |
                   (static_cast<uint32_t>(block[i * 4 + 1]) << 16) |
                   (static_cast<uint32_t>(block[i * 4 + 2]) << 8) |
                   static_cast<uint32_t>(block[i * 4 + 3]);
        }
        
        for (int i = 16; i < 64; i++) {
            uint32_t s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >> 3);
            uint32_t s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >> 10);
            w[i] = w[i - 16] + s0 + w[i - 7] + s1;
        }
        
        uint32_t a = state_[0], b = state_[1], c = state_[2], d = state_[3];
        uint32_t e = state_[4], f = state_[5], g = state_[6], h = state_[7];
        
        for (int i = 0; i < 64; i++) {
            uint32_t S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
            uint32_t ch = (e & f) ^ (~e & g);
            uint32_t temp1 = h + S1 + ch + K[i] + w[i];
            uint32_t S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
            uint32_t maj = (a & b) ^ (a & c) ^ (b & c);
            uint32_t temp2 = S0 + maj;
            
            h = g; g = f; f = e; e = d + temp1;
            d = c; c = b; b = a; a = temp1 + temp2;
        }
        
        state_[0] += a; state_[1] += b; state_[2] += c; state_[3] += d;
        state_[4] += e; state_[5] += f; state_[6] += g; state_[7] += h;
    }
};

// Convenience functions
inline std::string xxhash64(const std::string& str, uint64_t seed = 0) {
    return XXHash64(seed).hashHex(str);
}

inline std::string sha256(const std::string& str) {
    return SHA256().hashHex(str);
}

} // namespace hash
} // namespace aipr
