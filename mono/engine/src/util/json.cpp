/**
 * AI PR Reviewer - JSON Utilities
 * 
 * Minimal JSON parsing/serialization helpers.
 */

#include <string>
#include <vector>
#include <unordered_map>
#include <variant>
#include <sstream>
#include <stdexcept>
#include <cctype>
#include <memory>
#include <map>

namespace aipr {
namespace json {

/**
 * JSON value types
 */
using JsonNull = std::nullptr_t;
using JsonBool = bool;
using JsonNumber = double;
using JsonString = std::string;

// Forward declare - we use std::map instead of unordered_map to avoid
// incomplete-type issues with GCC 11's std::variant implementation.
class JsonValue;
using JsonArray = std::vector<JsonValue>;
using JsonObject = std::map<std::string, JsonValue>;

/**
 * Variant-based JSON value
 * 
 * Uses a two-phase approach: the variant holds a std::unique_ptr for
 * recursive container types (array/object) to avoid incomplete-type
 * issues across different compiler versions.
 */
class JsonValue {
public:
    using Value = std::variant<
        JsonNull, JsonBool, JsonNumber, JsonString,
        std::unique_ptr<JsonArray>,
        std::unique_ptr<JsonObject>
    >;
    
    JsonValue() : value_(nullptr) {}
    JsonValue(std::nullptr_t) : value_(nullptr) {}
    JsonValue(bool v) : value_(v) {}
    JsonValue(int v) : value_(static_cast<double>(v)) {}
    JsonValue(double v) : value_(v) {}
    JsonValue(const char* v) : value_(std::string(v)) {}
    JsonValue(const std::string& v) : value_(v) {}
    JsonValue(std::string&& v) : value_(std::move(v)) {}
    JsonValue(const JsonArray& v) : value_(std::make_unique<JsonArray>(v)) {}
    JsonValue(JsonArray&& v) : value_(std::make_unique<JsonArray>(std::move(v))) {}
    JsonValue(const JsonObject& v) : value_(std::make_unique<JsonObject>(v)) {}
    JsonValue(JsonObject&& v) : value_(std::make_unique<JsonObject>(std::move(v))) {}
    
    // Copy constructor/assignment (deep copy)
    JsonValue(const JsonValue& other) { copyFrom(other); }
    JsonValue& operator=(const JsonValue& other) {
        if (this != &other) copyFrom(other);
        return *this;
    }
    JsonValue(JsonValue&&) = default;
    JsonValue& operator=(JsonValue&&) = default;
    
    bool isNull() const { return std::holds_alternative<JsonNull>(value_); }
    bool isBool() const { return std::holds_alternative<JsonBool>(value_); }
    bool isNumber() const { return std::holds_alternative<JsonNumber>(value_); }
    bool isString() const { return std::holds_alternative<JsonString>(value_); }
    bool isArray() const { return std::holds_alternative<std::unique_ptr<JsonArray>>(value_); }
    bool isObject() const { return std::holds_alternative<std::unique_ptr<JsonObject>>(value_); }
    
    bool asBool() const { return std::get<JsonBool>(value_); }
    double asNumber() const { return std::get<JsonNumber>(value_); }
    const std::string& asString() const { return std::get<JsonString>(value_); }
    const JsonArray& asArray() const { return *std::get<std::unique_ptr<JsonArray>>(value_); }
    JsonArray& asArray() { return *std::get<std::unique_ptr<JsonArray>>(value_); }
    const JsonObject& asObject() const { return *std::get<std::unique_ptr<JsonObject>>(value_); }
    JsonObject& asObject() { return *std::get<std::unique_ptr<JsonObject>>(value_); }
    
    // Object access
    JsonValue& operator[](const std::string& key) {
        if (!isObject()) {
            value_ = std::make_unique<JsonObject>();
        }
        return (*std::get<std::unique_ptr<JsonObject>>(value_))[key];
    }
    
    const JsonValue& operator[](const std::string& key) const {
        static JsonValue null_value;
        if (!isObject()) return null_value;
        const auto& obj = *std::get<std::unique_ptr<JsonObject>>(value_);
        auto it = obj.find(key);
        return it != obj.end() ? it->second : null_value;
    }
    
    // Array access
    JsonValue& operator[](size_t index) {
        return (*std::get<std::unique_ptr<JsonArray>>(value_))[index];
    }
    
    const JsonValue& operator[](size_t index) const {
        return (*std::get<std::unique_ptr<JsonArray>>(value_))[index];
    }
    
    bool contains(const std::string& key) const {
        if (!isObject()) return false;
        return std::get<std::unique_ptr<JsonObject>>(value_)->count(key) > 0;
    }
    
    size_t size() const {
        if (isArray()) return std::get<std::unique_ptr<JsonArray>>(value_)->size();
        if (isObject()) return std::get<std::unique_ptr<JsonObject>>(value_)->size();
        return 0;
    }
    
private:
    Value value_;
    
    void copyFrom(const JsonValue& other) {
        if (other.isNull()) { value_ = nullptr; }
        else if (other.isBool()) { value_ = other.asBool(); }
        else if (other.isNumber()) { value_ = other.asNumber(); }
        else if (other.isString()) { value_ = other.asString(); }
        else if (other.isArray()) { value_ = std::make_unique<JsonArray>(other.asArray()); }
        else if (other.isObject()) { value_ = std::make_unique<JsonObject>(other.asObject()); }
    }
};

/**
 * JSON serializer
 */
class JsonWriter {
public:
    explicit JsonWriter(bool pretty = false, int indent = 2)
        : pretty_(pretty), indent_(indent), depth_(0) {}
    
    std::string write(const JsonValue& value) {
        output_.clear();
        depth_ = 0;
        writeValue(value);
        return output_;
    }
    
private:
    void writeValue(const JsonValue& value) {
        if (value.isNull()) {
            output_ += "null";
        } else if (value.isBool()) {
            output_ += value.asBool() ? "true" : "false";
        } else if (value.isNumber()) {
            double num = value.asNumber();
            if (num == static_cast<int64_t>(num)) {
                output_ += std::to_string(static_cast<int64_t>(num));
            } else {
                output_ += std::to_string(num);
            }
        } else if (value.isString()) {
            writeString(value.asString());
        } else if (value.isArray()) {
            writeArray(value.asArray());
        } else if (value.isObject()) {
            writeObject(value.asObject());
        }
    }
    
    void writeString(const std::string& str) {
        output_ += '"';
        for (char c : str) {
            switch (c) {
                case '"': output_ += "\\\""; break;
                case '\\': output_ += "\\\\"; break;
                case '\b': output_ += "\\b"; break;
                case '\f': output_ += "\\f"; break;
                case '\n': output_ += "\\n"; break;
                case '\r': output_ += "\\r"; break;
                case '\t': output_ += "\\t"; break;
                default:
                    if (static_cast<unsigned char>(c) < 0x20) {
                        char buf[8];
                        snprintf(buf, sizeof(buf), "\\u%04x", static_cast<unsigned char>(c));
                        output_ += buf;
                    } else {
                        output_ += c;
                    }
            }
        }
        output_ += '"';
    }
    
    void writeArray(const JsonArray& arr) {
        output_ += '[';
        depth_++;
        bool first = true;
        for (const auto& item : arr) {
            if (!first) output_ += ',';
            if (pretty_) {
                output_ += '\n';
                output_ += std::string(depth_ * indent_, ' ');
            }
            writeValue(item);
            first = false;
        }
        depth_--;
        if (!arr.empty() && pretty_) {
            output_ += '\n';
            output_ += std::string(depth_ * indent_, ' ');
        }
        output_ += ']';
    }
    
    void writeObject(const JsonObject& obj) {
        output_ += '{';
        depth_++;
        bool first = true;
        for (const auto& [key, value] : obj) {
            if (!first) output_ += ',';
            if (pretty_) {
                output_ += '\n';
                output_ += std::string(depth_ * indent_, ' ');
            }
            writeString(key);
            output_ += pretty_ ? ": " : ":";
            writeValue(value);
            first = false;
        }
        depth_--;
        if (!obj.empty() && pretty_) {
            output_ += '\n';
            output_ += std::string(depth_ * indent_, ' ');
        }
        output_ += '}';
    }
    
    std::string output_;
    bool pretty_;
    int indent_;
    int depth_;
};

/**
 * Simple JSON parser
 */
class JsonParser {
public:
    JsonValue parse(const std::string& json) {
        input_ = json;
        pos_ = 0;
        skipWhitespace();
        return parseValue();
    }
    
private:
    std::string input_;
    size_t pos_;
    
    char peek() const {
        return pos_ < input_.size() ? input_[pos_] : '\0';
    }
    
    char consume() {
        return pos_ < input_.size() ? input_[pos_++] : '\0';
    }
    
    void skipWhitespace() {
        while (pos_ < input_.size() && std::isspace(input_[pos_])) {
            pos_++;
        }
    }
    
    void expect(char c) {
        skipWhitespace();
        if (consume() != c) {
            throw std::runtime_error("Expected '" + std::string(1, c) + "'");
        }
    }
    
    JsonValue parseValue() {
        skipWhitespace();
        char c = peek();
        
        if (c == 'n') return parseNull();
        if (c == 't' || c == 'f') return parseBool();
        if (c == '"') return parseString();
        if (c == '[') return parseArray();
        if (c == '{') return parseObject();
        if (c == '-' || std::isdigit(c)) return parseNumber();
        
        throw std::runtime_error("Unexpected character: " + std::string(1, c));
    }
    
    JsonValue parseNull() {
        if (input_.substr(pos_, 4) == "null") {
            pos_ += 4;
            return JsonValue(nullptr);
        }
        throw std::runtime_error("Invalid null");
    }
    
    JsonValue parseBool() {
        if (input_.substr(pos_, 4) == "true") {
            pos_ += 4;
            return JsonValue(true);
        }
        if (input_.substr(pos_, 5) == "false") {
            pos_ += 5;
            return JsonValue(false);
        }
        throw std::runtime_error("Invalid boolean");
    }
    
    JsonValue parseString() {
        expect('"');
        std::string result;
        while (peek() != '"') {
            char c = consume();
            if (c == '\\') {
                c = consume();
                switch (c) {
                    case '"': result += '"'; break;
                    case '\\': result += '\\'; break;
                    case '/': result += '/'; break;
                    case 'b': result += '\b'; break;
                    case 'f': result += '\f'; break;
                    case 'n': result += '\n'; break;
                    case 'r': result += '\r'; break;
                    case 't': result += '\t'; break;
                    case 'u': {
                        // Skip unicode escape for simplicity
                        pos_ += 4;
                        result += '?';
                        break;
                    }
                    default: result += c;
                }
            } else {
                result += c;
            }
        }
        expect('"');
        return JsonValue(result);
    }
    
    JsonValue parseNumber() {
        size_t start = pos_;
        if (peek() == '-') pos_++;
        while (std::isdigit(peek())) pos_++;
        if (peek() == '.') {
            pos_++;
            while (std::isdigit(peek())) pos_++;
        }
        if (peek() == 'e' || peek() == 'E') {
            pos_++;
            if (peek() == '+' || peek() == '-') pos_++;
            while (std::isdigit(peek())) pos_++;
        }
        return JsonValue(std::stod(input_.substr(start, pos_ - start)));
    }
    
    JsonValue parseArray() {
        expect('[');
        JsonArray arr;
        skipWhitespace();
        if (peek() != ']') {
            arr.push_back(parseValue());
            skipWhitespace();
            while (peek() == ',') {
                consume();
                arr.push_back(parseValue());
                skipWhitespace();
            }
        }
        expect(']');
        return JsonValue(std::move(arr));
    }
    
    JsonValue parseObject() {
        expect('{');
        JsonObject obj;
        skipWhitespace();
        if (peek() != '}') {
            auto key = parseString().asString();
            expect(':');
            obj[key] = parseValue();
            skipWhitespace();
            while (peek() == ',') {
                consume();
                skipWhitespace();
                key = parseString().asString();
                expect(':');
                obj[key] = parseValue();
                skipWhitespace();
            }
        }
        expect('}');
        return JsonValue(std::move(obj));
    }
};

// Convenience functions
inline JsonValue parse(const std::string& json) {
    return JsonParser().parse(json);
}

inline std::string stringify(const JsonValue& value, bool pretty = false) {
    return JsonWriter(pretty).write(value);
}

} // namespace json
} // namespace aipr
