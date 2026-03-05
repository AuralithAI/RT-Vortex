package dev.rtvortex.sdk.model;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

@JsonIgnoreProperties(ignoreUnknown = true)
public class AdminStats {
    @JsonProperty("total_users")
    private int totalUsers;
    @JsonProperty("total_orgs")
    private int totalOrgs;
    @JsonProperty("total_repos")
    private int totalRepos;
    @JsonProperty("total_reviews")
    private int totalReviews;
    @JsonProperty("reviews_today")
    private int reviewsToday;
    @JsonProperty("active_jobs")
    private int activeJobs;

    public AdminStats() {}

    public int getTotalUsers() { return totalUsers; }
    public void setTotalUsers(int totalUsers) { this.totalUsers = totalUsers; }
    public int getTotalOrgs() { return totalOrgs; }
    public void setTotalOrgs(int totalOrgs) { this.totalOrgs = totalOrgs; }
    public int getTotalRepos() { return totalRepos; }
    public void setTotalRepos(int totalRepos) { this.totalRepos = totalRepos; }
    public int getTotalReviews() { return totalReviews; }
    public void setTotalReviews(int totalReviews) { this.totalReviews = totalReviews; }
    public int getReviewsToday() { return reviewsToday; }
    public void setReviewsToday(int reviewsToday) { this.reviewsToday = reviewsToday; }
    public int getActiveJobs() { return activeJobs; }
    public void setActiveJobs(int activeJobs) { this.activeJobs = activeJobs; }
}
