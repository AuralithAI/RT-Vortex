// ─── Embeddings Settings ─────────────────────────────────────────────────────
// Default: built-in MiniLM-L6-v2 via ONNX Runtime (no API key needed).
// When unchecked, shows available external embedding providers.
// Each provider has a pre-filled (but editable) endpoint URL and API key input.
// Selection + config is saved to the backend and propagated to the C++ engine via gRPC.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useState } from "react";
import { Cpu, Cloud, CheckCircle, Info, Loader2, Link, Key, Eye, EyeOff } from "lucide-react";
import { useEmbeddingsConfig } from "@/lib/api/queries";
import { useUpdateEmbeddings } from "@/lib/api/mutations";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export function EmbeddingsSettings() {
  const { data: config, isLoading } = useEmbeddingsConfig();
  const updateEmbeddings = useUpdateEmbeddings();

  const [useBuiltin, setUseBuiltin] = useState(true);
  const [selectedProvider, setSelectedProvider] = useState<string>("");
  // Per-provider endpoint URLs (pre-filled from backend, editable by user).
  const [endpoints, setEndpoints] = useState<Record<string, string>>({});
  // Dedicated embedding API key (separate from LLM key).
  const [embeddingApiKey, setEmbeddingApiKey] = useState("");
  const [showApiKey, setShowApiKey] = useState(false);

  // Sync state from backend on load.
  useEffect(() => {
    if (config) {
      setUseBuiltin(config.use_builtin);
      setSelectedProvider(config.active_provider || "");
      // Pre-fill endpoint URLs from backend response.
      const eps: Record<string, string> = {};
      for (const ep of config.external_providers ?? []) {
        eps[ep.name] = ep.endpoint;
      }
      setEndpoints(eps);
    }
  }, [config]);

  const handleToggleBuiltin = (checked: boolean) => {
    setUseBuiltin(checked);
    if (checked) {
      // Switch back to built-in — save immediately.
      updateEmbeddings.mutate({
        use_builtin: true,
      });
      setSelectedProvider("");
      setEmbeddingApiKey("");
    }
  };

  const handleSelectProvider = (providerName: string) => {
    setSelectedProvider(providerName);
    setEmbeddingApiKey(""); // reset key field when switching providers
  };

  const handleSaveProvider = () => {
    const ep = config?.external_providers?.find((p) => p.name === selectedProvider);
    if (!ep) return;
    updateEmbeddings.mutate({
      use_builtin: false,
      provider: ep.name,
      endpoint: endpoints[ep.name] || ep.endpoint,
      model: ep.model,
      dimensions: ep.dimensions,
      // Only send api_key if the user typed one; otherwise backend
      // auto-inherits from the LLM provider registry.
      ...(embeddingApiKey ? { api_key: embeddingApiKey } : {}),
    });
  };

  return (
    <Card className="max-w-3xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Cpu className="h-5 w-5" />
          Embedding Model
        </CardTitle>
        <CardDescription>
          Choose how code is converted into vector embeddings for semantic search
          and context retrieval during reviews. This setting is sent to the C++
          engine when indexing repositories.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {isLoading ? (
          <Skeleton className="h-40 w-full" />
        ) : (
          <>
            {/* Built-in toggle */}
            <div className="flex items-start justify-between rounded-lg border p-4">
              <div className="space-y-2 flex-1">
                <div className="flex items-center gap-2">
                  <Cpu className="h-4 w-4 text-primary" />
                  <Label htmlFor="builtin-toggle" className="text-sm font-semibold">
                    Use built-in {config?.builtin_model.name ?? "MiniLM-L6-v2"}
                  </Label>
                  <Badge variant="success" className="text-xs">
                    <CheckCircle className="mr-1 h-3 w-3" />
                    Recommended
                  </Badge>
                </div>
                <p className="text-xs text-muted-foreground ml-6">
                  {config?.builtin_model.description ??
                    "Lightweight local embedding model — no API key required. Runs on the C++ engine via ONNX Runtime."}
                </p>
                <div className="flex gap-3 ml-6 text-xs text-muted-foreground">
                  <span>
                    Provider:{" "}
                    <span className="font-medium text-foreground">
                      {config?.builtin_model.provider ??
                        "Sentence Transformers (HuggingFace)"}
                    </span>
                  </span>
                  <span>
                    Dimensions:{" "}
                    <span className="font-medium text-foreground">
                      {config?.builtin_model.dimensions ?? 384}
                    </span>
                  </span>
                </div>
              </div>
              <Switch
                id="builtin-toggle"
                checked={useBuiltin}
                onCheckedChange={handleToggleBuiltin}
                disabled={updateEmbeddings.isPending}
              />
            </div>

            {/* Info banner */}
            {useBuiltin && (
              <div className="flex items-start gap-2 rounded-md bg-blue-50 p-3 dark:bg-blue-950">
                <Info className="h-4 w-4 text-blue-600 dark:text-blue-400 mt-0.5 shrink-0" />
                <p className="text-xs text-blue-700 dark:text-blue-300">
                  The built-in model runs entirely on your server — no data leaves
                  your infrastructure. It processes embeddings locally via ONNX
                  Runtime in the C++ engine with zero external API calls.
                </p>
              </div>
            )}

            {/* External providers (shown when built-in is unchecked) */}
            {!useBuiltin && (
              <div className="space-y-4">
                <p className="text-sm font-medium flex items-center gap-2">
                  <Cloud className="h-4 w-4" />
                  External Embedding Providers
                </p>
                <p className="text-xs text-muted-foreground">
                  Use a cloud embedding API instead of the built-in model. Higher
                  dimensions may improve retrieval quality for large codebases.
                  Select a provider, verify the endpoint URL, and provide your API
                  token to activate.
                </p>

                {/* Provider cards */}
                <div className="space-y-2">
                  {(config?.external_providers ?? []).map((ep) => {
                    const isActive = selectedProvider === ep.name;

                    return (
                      <div key={ep.name} className="space-y-0">
                        <div
                          className={`flex items-center justify-between rounded-lg border p-3 cursor-pointer transition-colors ${
                            isActive
                              ? "border-primary bg-primary/5 rounded-b-none"
                              : "hover:border-muted-foreground/30"
                          }`}
                          onClick={() => handleSelectProvider(ep.name)}
                          role="button"
                          tabIndex={0}
                          onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") {
                              handleSelectProvider(ep.name);
                            }
                          }}
                        >
                          <div className="space-y-0.5">
                            <div className="flex items-center gap-2">
                              <p className="text-sm font-medium">{ep.display_name}</p>
                              {isActive && config?.active_provider === ep.name && (
                                <Badge variant="default" className="text-xs">
                                  Active
                                </Badge>
                              )}
                            </div>
                            <div className="flex gap-3 text-xs text-muted-foreground">
                              <span>Model: {ep.model}</span>
                              <span>Dimensions: {ep.dimensions}</span>
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            <Badge
                              variant={ep.configured ? "success" : "secondary"}
                              className="text-xs"
                            >
                              {ep.configured ? "API Key Set" : "No API Key"}
                            </Badge>
                          </div>
                        </div>

                        {/* Expanded config panel for selected provider */}
                        {isActive && (
                          <div className="rounded-b-lg border border-t-0 border-primary bg-primary/5 p-4 space-y-4">
                            {/* Endpoint URL (pre-filled, editable) */}
                            <div className="space-y-1.5">
                              <Label htmlFor={`endpoint-${ep.name}`} className="text-xs font-medium flex items-center gap-1.5">
                                <Link className="h-3.5 w-3.5" />
                                Embedding API Endpoint
                              </Label>
                              <Input
                                id={`endpoint-${ep.name}`}
                                value={endpoints[ep.name] || ""}
                                onChange={(e) =>
                                  setEndpoints((prev) => ({
                                    ...prev,
                                    [ep.name]: e.target.value,
                                  }))
                                }
                                placeholder={ep.endpoint}
                                className="font-mono text-xs h-8"
                              />
                              <p className="text-[10px] text-muted-foreground">
                                Pre-filled with the standard endpoint. Edit only if you use a proxy or custom deployment.
                              </p>
                            </div>

                            {/* API Key input */}
                            {ep.requires_key && (
                              <div className="space-y-1.5">
                                <Label htmlFor={`apikey-${ep.name}`} className="text-xs font-medium flex items-center gap-1.5">
                                  <Key className="h-3.5 w-3.5" />
                                  API Key
                                  {ep.configured && (
                                    <span className="text-[10px] text-muted-foreground font-normal">
                                      (leave blank to use the key from LLM settings)
                                    </span>
                                  )}
                                </Label>
                                <div className="relative">
                                  <Input
                                    id={`apikey-${ep.name}`}
                                    type={showApiKey ? "text" : "password"}
                                    value={embeddingApiKey}
                                    onChange={(e) => setEmbeddingApiKey(e.target.value)}
                                    placeholder={
                                      ep.configured
                                        ? "Using key from LLM settings (override here)"
                                        : "Enter your API key"
                                    }
                                    className="font-mono text-xs h-8 pr-9"
                                  />
                                  <button
                                    type="button"
                                    onClick={() => setShowApiKey(!showApiKey)}
                                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                                  >
                                    {showApiKey ? (
                                      <EyeOff className="h-3.5 w-3.5" />
                                    ) : (
                                      <Eye className="h-3.5 w-3.5" />
                                    )}
                                  </button>
                                </div>
                                {!ep.configured && !embeddingApiKey && (
                                  <p className="text-[10px] text-amber-600 dark:text-amber-400">
                                    No API key found in LLM settings for this provider. Please enter one above.
                                  </p>
                                )}
                              </div>
                            )}

                            {/* Save button */}
                            <Button
                              size="sm"
                              onClick={handleSaveProvider}
                              disabled={updateEmbeddings.isPending || (!ep.configured && !embeddingApiKey)}
                              className="w-full"
                            >
                              {updateEmbeddings.isPending ? (
                                <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
                              ) : (
                                <CheckCircle className="mr-2 h-3.5 w-3.5" />
                              )}
                              Save &amp; Activate {ep.display_name}
                            </Button>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>

                {(!config?.external_providers ||
                  config.external_providers.length === 0) && (
                  <p className="text-xs text-muted-foreground text-center py-4">
                    No external embedding providers available.
                  </p>
                )}

                {/* Save confirmation */}
                {updateEmbeddings.isSuccess && !useBuiltin && (
                  <div className="flex items-center gap-2 rounded-md bg-green-50 p-2 dark:bg-green-950 text-xs text-green-700 dark:text-green-300">
                    <CheckCircle className="h-3.5 w-3.5" />
                    Embedding provider saved. It will be used for the next indexing run.
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
