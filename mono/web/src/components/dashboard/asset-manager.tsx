// ─── Asset Manager ───────────────────────────────────────────────────────────
// Compact card grid for uploading, listing, and deleting multimodal assets
// (images, audio, PDFs, URLs) within a repository.  Paginated (12 per page).
// Includes inline previews: image thumbnails, audio players, PDF embeds.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useCallback, useRef, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
  Upload,
  Link2,
  Image as ImageIcon,
  Mic,
  FileText,
  Globe,
  Film,
  File,
  Trash2,
  Loader2,
  CheckCircle2,
  XCircle,
  ChevronLeft,
  ChevronRight,
  Plus,
  X,
  Eye,
  Play,
  ExternalLink,
} from "lucide-react";
import { useAssets } from "@/lib/api/queries";
import { useUploadAsset, useIngestUrl, useDeleteAsset } from "@/lib/api/mutations";
import { assets as assetsApi } from "@/lib/api/client";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { useUIStore } from "@/lib/stores/ui";
import type { Asset, AssetType } from "@/types/api";

// ── Constants ───────────────────────────────────────────────────────────────

const PAGE_SIZE = 12;

const ACCEPT_TYPES =
  "image/png,image/jpeg,image/webp,image/svg+xml,audio/wav,audio/mpeg,audio/ogg,audio/flac,application/pdf";

const ASSET_ICONS: Record<AssetType, typeof ImageIcon> = {
  image: ImageIcon,
  audio: Mic,
  pdf: FileText,
  webpage: Globe,
  video: Film,
  document: File,
};

const ASSET_COLORS: Record<AssetType, string> = {
  image: "text-violet-500",
  audio: "text-amber-500",
  pdf: "text-red-500",
  webpage: "text-blue-500",
  video: "text-pink-500",
  document: "text-zinc-500",
};

// ── Helpers ─────────────────────────────────────────────────────────────────

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  const days = Math.floor(hrs / 24);
  return `${days}d ago`;
}

// ── Component ───────────────────────────────────────────────────────────────

interface AssetManagerProps {
  repoId: string;
}

export function AssetManager({ repoId }: AssetManagerProps) {
  const { data: assets, isLoading } = useAssets(repoId);
  const uploadAsset = useUploadAsset();
  const ingestUrl = useIngestUrl();
  const deleteAsset = useDeleteAsset();
  const { showConfirm, addToast } = useUIStore();

  const fileInputRef = useRef<HTMLInputElement>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [urlInput, setUrlInput] = useState("");
  const [showUrlInput, setShowUrlInput] = useState(false);
  const [page, setPage] = useState(0);

  // ── Upload handling ───────────────────────────────────────────────────────

  const handleFiles = useCallback(
    (files: FileList | File[]) => {
      const arr = Array.from(files);
      if (arr.length === 0) return;
      for (const file of arr) {
        uploadAsset.mutate(
          { repoId, file },
          {
            onSuccess: () => {
              addToast({
                title: `Uploaded ${file.name}`,
                variant: "success",
              });
            },
            onError: () => {
              addToast({
                title: `Failed to upload ${file.name}`,
                variant: "error",
              });
            },
          },
        );
      }
      // Reset to first page so user sees the new uploads
      setPage(0);
    },
    [repoId, uploadAsset, addToast],
  );

  const onDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(true);
  }, []);

  const onDragLeave = useCallback(() => setIsDragging(false), []);

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setIsDragging(false);
      handleFiles(e.dataTransfer.files);
    },
    [handleFiles],
  );

  const onFileChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      if (e.target.files) handleFiles(e.target.files);
      e.target.value = ""; // reset so same file can be re-selected
    },
    [handleFiles],
  );

  // ── URL ingest ────────────────────────────────────────────────────────────

  const handleIngestUrl = useCallback(() => {
    const url = urlInput.trim();
    if (!url) return;
    ingestUrl.mutate(
      { repoId, url },
      {
        onSuccess: () => {
          addToast({ title: "URL queued for ingestion", variant: "success" });
          setUrlInput("");
          setShowUrlInput(false);
          setPage(0);
        },
        onError: () => {
          addToast({ title: "Failed to ingest URL", variant: "error" });
        },
      },
    );
  }, [repoId, urlInput, ingestUrl, addToast]);

  // ── Delete ────────────────────────────────────────────────────────────────

  const handleDelete = useCallback(
    (asset: Asset) => {
      showConfirm(
        "Delete Asset",
        `Remove "${asset.file_name || asset.source_url || asset.id}"? The embedding will also be removed.`,
        () => {
          deleteAsset.mutate(
            { repoId, assetId: asset.id },
            {
              onSuccess: () =>
                addToast({ title: "Asset deleted", variant: "success" }),
              onError: () =>
                addToast({ title: "Failed to delete asset", variant: "error" }),
            },
          );
        },
      );
    },
    [repoId, deleteAsset, showConfirm, addToast],
  );

  // ── Pagination ────────────────────────────────────────────────────────────

  const allAssets: Asset[] = assets ?? [];
  const totalPages = Math.max(1, Math.ceil(allAssets.length / PAGE_SIZE));
  const safePage = Math.min(page, totalPages - 1);
  const pagedAssets = allAssets.slice(
    safePage * PAGE_SIZE,
    safePage * PAGE_SIZE + PAGE_SIZE,
  );

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          <Upload className="h-4 w-4" />
          Assets
          {allAssets.length > 0 && (
            <Badge variant="secondary" className="text-[10px] ml-1">
              {allAssets.length}
            </Badge>
          )}
        </CardTitle>

        <div className="flex items-center gap-1.5">
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs gap-1"
            onClick={() => setShowUrlInput(!showUrlInput)}
          >
            <Link2 className="h-3.5 w-3.5" />
            URL
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="h-7 text-xs gap-1"
            onClick={() => fileInputRef.current?.click()}
            disabled={uploadAsset.isPending}
          >
            {uploadAsset.isPending ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Plus className="h-3.5 w-3.5" />
            )}
            Upload
          </Button>
          <input
            ref={fileInputRef}
            type="file"
            className="hidden"
            accept={ACCEPT_TYPES}
            multiple
            onChange={onFileChange}
          />
        </div>
      </CardHeader>

      <CardContent className="space-y-3">
        {/* URL ingest input */}
        <AnimatePresence>
          {showUrlInput && (
            <motion.div
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: 0.15 }}
              className="flex gap-2"
            >
              <Input
                placeholder="https://example.com/doc.pdf"
                value={urlInput}
                onChange={(e) => setUrlInput(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleIngestUrl()}
                className="h-8 text-xs flex-1"
              />
              <Button
                size="sm"
                className="h-8 text-xs"
                onClick={handleIngestUrl}
                disabled={!urlInput.trim() || ingestUrl.isPending}
              >
                {ingestUrl.isPending ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  "Ingest"
                )}
              </Button>
            </motion.div>
          )}
        </AnimatePresence>

        {/* Drop zone (shown when empty or dragging) */}
        {(allAssets.length === 0 || isDragging) && (
          <div
            onDragOver={onDragOver}
            onDragLeave={onDragLeave}
            onDrop={onDrop}
            onClick={() => fileInputRef.current?.click()}
            className={`flex flex-col items-center justify-center gap-1.5 rounded-lg border-2 border-dashed p-6 cursor-pointer transition-colors ${
              isDragging
                ? "border-primary bg-primary/5"
                : "border-muted-foreground/20 hover:border-muted-foreground/40"
            }`}
          >
            <Upload
              className={`h-6 w-6 ${isDragging ? "text-primary" : "text-muted-foreground/50"}`}
            />
            <p className="text-xs text-muted-foreground">
              {isDragging
                ? "Drop files here"
                : "Drop images, audio, or PDFs — or click to browse"}
            </p>
            <p className="text-[10px] text-muted-foreground/60">
              PNG · JPEG · WebP · WAV · MP3 · OGG · FLAC · PDF
            </p>
          </div>
        )}

        {/* Loading */}
        {isLoading && (
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
            {[...Array(6)].map((_, i) => (
              <Skeleton key={i} className="h-16 rounded-lg" />
            ))}
          </div>
        )}

        {/* Asset grid */}
        {!isLoading && pagedAssets.length > 0 && (
          <>
            {/* Hidden drop catcher when assets exist */}
            <div
              onDragOver={onDragOver}
              onDragLeave={onDragLeave}
              onDrop={onDrop}
              className="absolute inset-0 pointer-events-none"
              style={{ pointerEvents: isDragging ? "auto" : "none" }}
            />

            <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
              <AnimatePresence mode="popLayout">
                {pagedAssets.map((asset) => (
                  <AssetCard
                    key={asset.id}
                    asset={asset}
                    repoId={repoId}
                    onDelete={handleDelete}
                    deleting={deleteAsset.isPending}
                  />
                ))}
              </AnimatePresence>
            </div>

            {/* Pagination */}
            {totalPages > 1 && (
              <div className="flex items-center justify-between pt-1">
                <p className="text-[10px] text-muted-foreground">
                  {safePage * PAGE_SIZE + 1}–
                  {Math.min((safePage + 1) * PAGE_SIZE, allAssets.length)} of{" "}
                  {allAssets.length}
                </p>
                <div className="flex gap-1">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 w-6 p-0"
                    onClick={() => setPage(Math.max(0, safePage - 1))}
                    disabled={safePage === 0}
                  >
                    <ChevronLeft className="h-3.5 w-3.5" />
                  </Button>
                  <span className="text-[10px] text-muted-foreground leading-6 px-1">
                    {safePage + 1}/{totalPages}
                  </span>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 w-6 p-0"
                    onClick={() =>
                      setPage(Math.min(totalPages - 1, safePage + 1))
                    }
                    disabled={safePage >= totalPages - 1}
                  >
                    <ChevronRight className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}

// ── Single Asset Card (with inline previews) ───────────────────────────────

interface AssetCardProps {
  asset: Asset;
  repoId: string;
  onDelete: (a: Asset) => void;
  deleting: boolean;
}

function AssetCard({ asset, repoId, onDelete, deleting }: AssetCardProps) {
  const [showPreview, setShowPreview] = useState(false);
  const Icon = ASSET_ICONS[asset.asset_type] ?? File;
  const iconColor = ASSET_COLORS[asset.asset_type] ?? "text-zinc-500";

  const displayName =
    asset.file_name ||
    (asset.source_url
      ? new URL(asset.source_url).hostname
      : asset.id.slice(0, 8));

  const contentUrl = assetsApi.contentUrl(repoId, asset.id);

  // Parse thumbnail from metadata JSON (stored at upload for images).
  let thumbnail: string | undefined;
  if (asset.metadata) {
    try {
      const meta = JSON.parse(asset.metadata);
      thumbnail = meta.thumbnail;
    } catch {
      // ignore
    }
  }

  return (
    <>
      <motion.div
        layout
        initial={{ opacity: 0, scale: 0.95 }}
        animate={{ opacity: 1, scale: 1 }}
        exit={{ opacity: 0, scale: 0.95 }}
        transition={{ duration: 0.15 }}
        className="group relative flex flex-col rounded-lg border bg-background overflow-hidden hover:bg-muted/40 transition-colors cursor-pointer"
        onClick={() => setShowPreview(true)}
      >
        {/* Preview thumbnail area */}
        <div className="relative h-24 bg-muted/30 flex items-center justify-center overflow-hidden">
          {asset.asset_type === "image" && thumbnail ? (
            <img
              src={thumbnail}
              alt={displayName}
              className="w-full h-full object-cover"
              loading="lazy"
            />
          ) : asset.asset_type === "image" ? (
            <img
              src={contentUrl}
              alt={displayName}
              className="w-full h-full object-cover"
              loading="lazy"
              onError={(e) => {
                // Fallback to icon if content endpoint isn't available
                (e.target as HTMLImageElement).style.display = "none";
              }}
            />
          ) : asset.asset_type === "audio" ? (
            <div className="flex flex-col items-center gap-1">
              <div className="w-10 h-10 rounded-full bg-amber-500/10 flex items-center justify-center">
                <Play className="h-5 w-5 text-amber-500" />
              </div>
              <span className="text-[9px] text-muted-foreground">Audio</span>
            </div>
          ) : asset.asset_type === "webpage" ? (
            <div className="flex flex-col items-center gap-1">
              <Globe className="h-8 w-8 text-blue-400" />
              {asset.source_url && (
                <span className="text-[9px] text-muted-foreground truncate max-w-[90%]">
                  {new URL(asset.source_url).hostname}
                </span>
              )}
            </div>
          ) : asset.asset_type === "pdf" ? (
            <div className="flex flex-col items-center gap-1">
              <FileText className="h-8 w-8 text-red-400" />
              <span className="text-[9px] text-muted-foreground">PDF Document</span>
            </div>
          ) : (
            <Icon className={`h-8 w-8 ${iconColor} opacity-60`} />
          )}

          {/* Hover overlay */}
          <div className="absolute inset-0 bg-black/0 group-hover:bg-black/20 transition-colors flex items-center justify-center">
            <Eye className="h-5 w-5 text-white opacity-0 group-hover:opacity-80 transition-opacity" />
          </div>
        </div>

        {/* Info bar */}
        <div className="p-2 flex items-center gap-2">
          <div className={`shrink-0 ${iconColor}`}>
            <Icon className="h-3.5 w-3.5" />
          </div>
          <div className="flex-1 min-w-0">
            <p
              className="text-[11px] font-medium truncate leading-tight"
              title={displayName}
            >
              {displayName}
            </p>
            <div className="flex items-center gap-1 mt-0.5">
              {asset.status === "ready" && (
                <CheckCircle2 className="h-2.5 w-2.5 text-green-500 shrink-0" />
              )}
              {asset.status === "processing" && (
                <Loader2 className="h-2.5 w-2.5 animate-spin text-amber-500 shrink-0" />
              )}
              {asset.status === "error" && (
                <XCircle className="h-2.5 w-2.5 text-red-500 shrink-0" />
              )}
              <span className="text-[9px] text-muted-foreground truncate">
                {formatBytes(asset.size_bytes)} · {timeAgo(asset.created_at)}
              </span>
            </div>
          </div>
        </div>

        {/* Delete button — shown on hover */}
        <button
          onClick={(e) => {
            e.stopPropagation();
            onDelete(asset);
          }}
          disabled={deleting}
          className="absolute top-1 right-1 p-1 rounded-md opacity-0 group-hover:opacity-100 bg-black/40 hover:bg-destructive/80 text-white transition-all z-10"
          title="Delete asset"
        >
          <Trash2 className="h-3 w-3" />
        </button>
      </motion.div>

      {/* Preview Dialog */}
      <AnimatePresence>
        {showPreview && (
          <AssetPreviewDialog
            asset={asset}
            repoId={repoId}
            contentUrl={contentUrl}
            onClose={() => setShowPreview(false)}
          />
        )}
      </AnimatePresence>
    </>
  );
}

// ── Asset Preview Dialog ────────────────────────────────────────────────────

function AssetPreviewDialog({
  asset,
  repoId,
  contentUrl,
  onClose,
}: {
  asset: Asset;
  repoId: string;
  contentUrl: string;
  onClose: () => void;
}) {
  const displayName =
    asset.file_name ||
    (asset.source_url ? new URL(asset.source_url).hostname : asset.id.slice(0, 8));

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onClick={onClose}
    >
      <motion.div
        initial={{ scale: 0.95, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        exit={{ scale: 0.95, opacity: 0 }}
        transition={{ duration: 0.15 }}
        className="bg-background border rounded-xl shadow-2xl max-w-2xl w-full max-h-[80vh] overflow-hidden flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b">
          <div className="flex items-center gap-2 min-w-0">
            <Badge variant="outline" className="text-[10px] shrink-0">
              {asset.asset_type.toUpperCase()}
            </Badge>
            <span className="text-sm font-medium truncate">{displayName}</span>
            <span className="text-xs text-muted-foreground shrink-0">
              {formatBytes(asset.size_bytes)}
            </span>
          </div>
          <div className="flex items-center gap-1 shrink-0">
            {asset.source_url && (
              <a
                href={asset.source_url}
                target="_blank"
                rel="noopener noreferrer"
                className="p-1.5 rounded-md hover:bg-muted transition-colors"
              >
                <ExternalLink className="h-4 w-4 text-muted-foreground" />
              </a>
            )}
            <button
              onClick={onClose}
              className="p-1.5 rounded-md hover:bg-muted transition-colors"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* Content area */}
        <div className="flex-1 overflow-auto p-4 flex items-center justify-center min-h-[200px]">
          {asset.asset_type === "image" && (
            <img
              src={contentUrl}
              alt={displayName}
              className="max-w-full max-h-[60vh] object-contain rounded-lg shadow-md"
            />
          )}

          {asset.asset_type === "audio" && (
            <div className="w-full space-y-4">
              <div className="flex items-center justify-center">
                <div className="w-20 h-20 rounded-full bg-amber-500/10 flex items-center justify-center">
                  <Mic className="h-10 w-10 text-amber-500" />
                </div>
              </div>
              <audio
                controls
                className="w-full"
                src={contentUrl}
                preload="metadata"
              >
                Your browser does not support the audio element.
              </audio>
              <p className="text-xs text-muted-foreground text-center">
                {asset.mime_type} · {formatBytes(asset.size_bytes)}
              </p>
            </div>
          )}

          {asset.asset_type === "pdf" && (
            <div className="w-full h-[60vh]">
              <iframe
                src={contentUrl}
                className="w-full h-full rounded-lg border"
                title={`PDF preview: ${displayName}`}
              />
            </div>
          )}

          {asset.asset_type === "webpage" && (
            <div className="text-center space-y-4">
              <Globe className="h-16 w-16 text-blue-400 mx-auto" />
              <div>
                <p className="text-sm font-medium">{displayName}</p>
                {asset.source_url && (
                  <a
                    href={asset.source_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-xs text-blue-500 hover:underline"
                  >
                    {asset.source_url}
                  </a>
                )}
              </div>
              <p className="text-xs text-muted-foreground">
                Content has been extracted and embedded for semantic search.
              </p>
            </div>
          )}

          {asset.asset_type === "video" && (
            <video
              controls
              className="max-w-full max-h-[60vh] rounded-lg shadow-md"
              src={contentUrl}
              preload="metadata"
            >
              Your browser does not support the video element.
            </video>
          )}

          {asset.asset_type === "document" && (
            <div className="text-center space-y-4">
              <File className="h-16 w-16 text-zinc-400 mx-auto" />
              <div>
                <p className="text-sm font-medium">{displayName}</p>
                <p className="text-xs text-muted-foreground mt-1">
                  {asset.mime_type} · {formatBytes(asset.size_bytes)}
                </p>
              </div>
              <p className="text-xs text-muted-foreground">
                Content has been extracted and embedded for semantic search.
              </p>
            </div>
          )}
        </div>

        {/* Footer with status + metadata */}
        <div className="flex items-center justify-between px-4 py-2.5 border-t bg-muted/30">
          <div className="flex items-center gap-2">
            {asset.status === "ready" && (
              <Badge className="bg-green-500/10 text-green-600 border-green-500/20 text-[10px]">
                <CheckCircle2 className="h-3 w-3 mr-1" />
                Embedded
              </Badge>
            )}
            {asset.status === "processing" && (
              <Badge className="bg-amber-500/10 text-amber-600 border-amber-500/20 text-[10px]">
                <Loader2 className="h-3 w-3 mr-1 animate-spin" />
                Processing
              </Badge>
            )}
            {asset.status === "error" && (
              <Badge className="bg-red-500/10 text-red-600 border-red-500/20 text-[10px]">
                <XCircle className="h-3 w-3 mr-1" />
                {asset.error_message || "Error"}
              </Badge>
            )}
            {asset.chunks_count > 0 && (
              <span className="text-[10px] text-muted-foreground">
                {asset.chunks_count} chunks indexed
              </span>
            )}
          </div>
          <span className="text-[10px] text-muted-foreground">
            {timeAgo(asset.created_at)}
          </span>
        </div>
      </motion.div>
    </motion.div>
  );
}
