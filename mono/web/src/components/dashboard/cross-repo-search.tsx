// ─── Cross-Repo Federated Search ─────────────────────────────────────────────
// UI for querying across all linked repos from the current repo context.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import {
  Search,
  Loader2,
  FileCode,
  ExternalLink,
  BarChart3,
  Clock,
} from "lucide-react";
import { useFederatedSearch } from "@/lib/api/mutations";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useUIStore } from "@/lib/stores/ui";
import type { FederatedSearchResponse, FederatedChunk } from "@/types/api";

interface CrossRepoSearchProps {
  repoId: string;
}

export function CrossRepoSearch({ repoId }: CrossRepoSearchProps) {
  const search = useFederatedSearch();
  const { addToast } = useUIStore();

  const [query, setQuery] = useState("");
  const [topK, setTopK] = useState(10);
  const [result, setResult] = useState<FederatedSearchResponse | null>(null);

  const handleSearch = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!query.trim()) return;
    try {
      const res = await search.mutateAsync({
        repoId,
        data: { query: query.trim(), top_k: topK },
      });
      setResult(res);
    } catch (err) {
      addToast({
        title: "Federated search failed",
        description: err instanceof Error ? err.message : "Unknown error",
        variant: "error",
      });
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Search className="h-4 w-4" />
          Federated Search
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Search form */}
        <form onSubmit={handleSearch} className="flex gap-2">
          <Input
            placeholder="Search across linked repos…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="flex-1"
          />
          <Input
            type="number"
            min={1}
            max={100}
            value={topK}
            onChange={(e) => setTopK(Number(e.target.value))}
            className="w-20"
            title="Top K results"
          />
          <Button type="submit" disabled={search.isPending}>
            {search.isPending ? (
              <Loader2 className="mr-1 h-4 w-4 animate-spin" />
            ) : (
              <Search className="mr-1 h-4 w-4" />
            )}
            Search
          </Button>
        </form>

        {/* Loading */}
        {search.isPending && (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-20 w-full" />
            ))}
          </div>
        )}

        {/* Results */}
        {result && !search.isPending && (
          <div className="space-y-4">
            {/* Metrics summary */}
            {result.metrics && (
              <div className="flex flex-wrap gap-3 rounded-lg border bg-muted/30 p-3 text-xs">
                <div className="flex items-center gap-1">
                  <BarChart3 className="h-3 w-3 text-muted-foreground" />
                  <span className="text-muted-foreground">
                    {result.metrics.repos_searched} repos searched
                  </span>
                </div>
                <div className="flex items-center gap-1">
                  <Clock className="h-3 w-3 text-muted-foreground" />
                  <span className="text-muted-foreground">
                    {result.metrics.total_search_time_ms}ms
                  </span>
                </div>
                <div className="flex items-center gap-1">
                  <span className="text-muted-foreground">
                    {result.metrics.total_candidates} candidates →{" "}
                    {result.chunks?.length ?? 0} results
                  </span>
                </div>
                {result.repos_denied > 0 && (
                  <Badge variant="destructive" className="text-[10px]">
                    {result.repos_denied} repos denied
                  </Badge>
                )}
              </div>
            )}

            {/* Chunks */}
            {result.chunks?.length === 0 ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                No results found across linked repos.
              </p>
            ) : (
              <div className="space-y-3">
                {result.chunks?.map((chunk: FederatedChunk, idx: number) => (
                  <ChunkCard key={`${chunk.repo_id}-${chunk.chunk.id}-${idx}`} chunk={chunk} />
                ))}
              </div>
            )}
          </div>
        )}

        {/* Empty state (no search yet) */}
        {!result && !search.isPending && (
          <div className="flex flex-col items-center gap-2 py-6 text-center">
            <Search className="h-8 w-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">
              Search code, symbols, and documentation across all linked
              repositories.
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ── Chunk result card ───────────────────────────────────────────────────────

function ChunkCard({ chunk }: { chunk: FederatedChunk }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="rounded-lg border bg-card">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-start justify-between gap-3 p-3 text-left hover:bg-muted/30"
      >
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <FileCode className="h-4 w-4 shrink-0 text-muted-foreground" />
            <span className="truncate text-sm font-medium">
              {chunk.chunk.file_path}
            </span>
            <Badge variant="outline" className="shrink-0 text-[10px]">
              {chunk.chunk.language}
            </Badge>
          </div>
          <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
            <ExternalLink className="h-3 w-3" />
            <span>{chunk.repo_name}</span>
            <span>
              L{chunk.chunk.start_line}–{chunk.chunk.end_line}
            </span>
          </div>
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1">
          <Badge
            variant={chunk.normalized_score > 0.7 ? "success" : chunk.normalized_score > 0.4 ? "default" : "secondary"}
            className="text-[10px]"
          >
            {(chunk.normalized_score * 100).toFixed(0)}%
          </Badge>
          {chunk.chunk.symbols?.length > 0 && (
            <span className="text-[10px] text-muted-foreground">
              {chunk.chunk.symbols.length} symbol
              {chunk.chunk.symbols.length !== 1 ? "s" : ""}
            </span>
          )}
        </div>
      </button>
      {expanded && (
        <div className="border-t px-3 pb-3 pt-2">
          <pre className="max-h-60 overflow-auto rounded bg-muted p-2 text-xs">
            <code>{chunk.chunk.content}</code>
          </pre>
          {chunk.chunk.symbols?.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1">
              {chunk.chunk.symbols.map((sym: string) => (
                <Badge key={sym} variant="outline" className="text-[10px]">
                  {sym}
                </Badge>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
