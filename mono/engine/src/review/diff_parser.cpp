/**
 * AI PR Reviewer - Diff Parser
 * 
 * Parses unified diff format into structured data.
 */

#include "types.h"
#include <string>
#include <vector>
#include <regex>
#include <sstream>

namespace aipr {

/**
 * Parse unified diff format
 */
class DiffParser {
public:
    ParsedDiff parse(const std::string& diff) {
        ParsedDiff result;
        
        std::istringstream stream(diff);
        std::string line;
        
        DiffHunk current_hunk;
        FileInfo current_file;
        bool in_hunk = false;
        std::string current_old_path;
        std::string current_new_path;
        
        // Regex patterns
        std::regex diff_header(R"(^diff --git a/(.+) b/(.+)$)");
        std::regex old_file(R"(^--- (.+)$)");
        std::regex new_file(R"(^\+\+\+ (.+)$)");
        std::regex hunk_header(R"(^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$)");
        std::regex rename_from(R"(^rename from (.+)$)");
        std::regex rename_to(R"(^rename to (.+)$)");
        std::regex new_file_mode(R"(^new file mode)");
        std::regex deleted_file_mode(R"(^deleted file mode)");
        
        while (std::getline(stream, line)) {
            std::smatch match;
            
            // Diff header
            if (std::regex_match(line, match, diff_header)) {
                // Save previous hunk if exists
                if (in_hunk && !current_hunk.content.empty()) {
                    result.hunks.push_back(current_hunk);
                }
                if (!current_new_path.empty()) {
                    current_file.path = current_new_path;
                    result.changed_files.push_back(current_file);
                }
                
                // Reset
                current_old_path = match[1].str();
                current_new_path = match[2].str();
                current_file = FileInfo();
                current_file.path = current_new_path;
                current_file.change_type = ChangeType::Modified;
                current_hunk = DiffHunk();
                in_hunk = false;
                continue;
            }
            
            // File mode changes
            if (std::regex_search(line, new_file_mode)) {
                current_file.change_type = ChangeType::Added;
                continue;
            }
            if (std::regex_search(line, deleted_file_mode)) {
                current_file.change_type = ChangeType::Deleted;
                continue;
            }
            
            // Rename detection
            if (std::regex_match(line, match, rename_from)) {
                current_file.change_type = ChangeType::Renamed;
                continue;
            }
            
            // Hunk header
            if (std::regex_match(line, match, hunk_header)) {
                // Save previous hunk
                if (in_hunk && !current_hunk.content.empty()) {
                    result.hunks.push_back(current_hunk);
                }
                
                current_hunk = DiffHunk();
                current_hunk.file_path = current_new_path;
                current_hunk.old_start = std::stoul(match[1].str());
                current_hunk.old_lines = match[2].matched ? std::stoul(match[2].str()) : 1;
                current_hunk.new_start = std::stoul(match[3].str());
                current_hunk.new_lines = match[4].matched ? std::stoul(match[4].str()) : 1;
                current_hunk.content = line + "\n";
                in_hunk = true;
                continue;
            }
            
            // Hunk content
            if (in_hunk) {
                current_hunk.content += line + "\n";
                
                if (!line.empty()) {
                    if (line[0] == '+') {
                        current_hunk.added_lines.push_back(line.substr(1));
                        result.total_additions++;
                    } else if (line[0] == '-') {
                        current_hunk.removed_lines.push_back(line.substr(1));
                        result.total_deletions++;
                    }
                }
            }
        }
        
        // Save final hunk and file
        if (in_hunk && !current_hunk.content.empty()) {
            result.hunks.push_back(current_hunk);
        }
        if (!current_new_path.empty()) {
            result.changed_files.push_back(current_file);
        }
        
        return result;
    }
    
    /**
     * Get changed line numbers from a diff
     */
    std::vector<std::pair<size_t, size_t>> getChangedLineRanges(
        const ParsedDiff& diff,
        const std::string& file_path
    ) {
        std::vector<std::pair<size_t, size_t>> ranges;
        
        for (const auto& hunk : diff.hunks) {
            if (hunk.file_path == file_path) {
                ranges.push_back({hunk.new_start, hunk.new_start + hunk.new_lines});
            }
        }
        
        return ranges;
    }
};

} // namespace aipr
