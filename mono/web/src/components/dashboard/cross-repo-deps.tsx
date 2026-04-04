// ─── Cross-Repo Dependencies Panel ──────────────────────────────────────────
// Displays cross-repo dependency edges and manifest information for a repo.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import {
  GitFork,
  FileCode,
  ArrowRight,
  Package,
  Code2,
  Layers,
} from "lucide-react";
import { useCrossRepoDeps, useCrossRepoManifest } from "@/lib/api/queries";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import type { CrossRepoDependency } from "@/types/api";

interface CrossRepoDepsProps {
  repoId: string;
}

export function CrossRepoDeps({ repoId }: CrossRepoDepsProps) {
  const { data: manifest, isLoading: manifestLoading } =
    useCrossRepoManifest(repoId);
  const { data: deps, isLoading: depsLoading } = useCrossRepoDeps(repoId);

  return (
    <div className="space-y-4">
      {/* Manifest Card */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Package className="h-4 w-4" />
            Repo Manifest
          </CardTitle>
        </CardHeader>
        <CardContent>
          {manifestLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-5 w-full" />
              ))}
            </div>
          ) : !manifest?.found ? (
            <p className="py-4 text-center text-sm text-muted-foreground">
              No manifest available. The repo may not be indexed yet.
            </p>
          ) : (
            <div className="space-y-3">
              <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-3">
                <ManifestField
                  label="Language"
                  value={manifest.primary_language}
                  icon={<Code2 className="h-3 w-3" />}
                />
                <ManifestField
                  label="Build System"
                  value={manifest.build_system}
                  icon={<Layers className="h-3 w-3" />}
                />
                <ManifestField
                  label="Repo Type"
                  value={manifest.repo_type}
                  icon={<Package className="h-3 w-3" />}
                />
              </div>

              {manifest.targets?.length > 0 && (
                <div>
                  <h4 className="mb-1 text-xs font-medium text-muted-foreground">
                    Build Targets ({manifest.targets.length})
                  </h4>
                  <div className="flex flex-wrap gap-1">
                    {manifest.targets.map((t) => (
                      <Badge key={t.name} variant="outline" className="text-xs">
                        {t.name}
                        <span className="ml-1 text-muted-foreground">
                          ({t.type})
                        </span>
                      </Badge>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Dependencies Card */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <GitFork className="h-4 w-4" />
            Cross-Repo Dependencies
            {deps && (
              <Badge variant="secondary" className="ml-1 text-xs">
                {deps.total_edges} edges
              </Badge>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {depsLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : !deps?.dependencies?.length ? (
            <p className="py-4 text-center text-sm text-muted-foreground">
              No cross-repo dependencies detected. Link more repos and
              re-build the graph.
            </p>
          ) : (
            <div className="space-y-2">
              {deps.repos_denied > 0 && (
                <p className="text-xs text-orange-600">
                  {deps.repos_denied} linked repo(s) were not accessible.
                </p>
              )}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Source</TableHead>
                    <TableHead />
                    <TableHead>Target</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Confidence</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {deps.dependencies.map(
                    (dep: CrossRepoDependency, idx: number) => (
                      <TableRow
                        key={`${dep.source_symbol}-${dep.target_symbol}-${idx}`}
                      >
                        <TableCell>
                          <div>
                            <p className="text-xs font-medium">
                              {dep.source_symbol}
                            </p>
                            <p className="flex items-center gap-1 text-[10px] text-muted-foreground">
                              <FileCode className="h-3 w-3" />
                              {dep.source_file}
                            </p>
                          </div>
                        </TableCell>
                        <TableCell className="px-1">
                          <ArrowRight className="h-3 w-3 text-muted-foreground" />
                        </TableCell>
                        <TableCell>
                          <div>
                            <p className="text-xs font-medium">
                              {dep.target_symbol}
                            </p>
                            <p className="flex items-center gap-1 text-[10px] text-muted-foreground">
                              <FileCode className="h-3 w-3" />
                              {dep.target_file}
                            </p>
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline" className="text-[10px]">
                            {dep.dependency_type}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <ConfidenceBar value={dep.confidence} />
                        </TableCell>
                      </TableRow>
                    ),
                  )}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ── Helpers ─────────────────────────────────────────────────────────────────

function ManifestField({
  label,
  value,
  icon,
}: {
  label: string;
  value: string;
  icon: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border bg-muted/30 p-2">
      <p className="flex items-center gap-1 text-[10px] text-muted-foreground">
        {icon}
        {label}
      </p>
      <p className="text-sm font-medium">{value || "—"}</p>
    </div>
  );
}

function ConfidenceBar({ value }: { value: number }) {
  const pct = Math.round(value * 100);
  const color =
    pct >= 80
      ? "bg-green-500"
      : pct >= 50
        ? "bg-yellow-500"
        : "bg-red-400";
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-16 rounded-full bg-muted">
        <div
          className={`h-full rounded-full ${color}`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="text-[10px] text-muted-foreground">{pct}%</span>
    </div>
  );
}
