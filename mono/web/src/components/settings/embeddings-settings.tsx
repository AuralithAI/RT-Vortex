// ─── Embeddings Settings ─────────────────────────────────────────────────────
// Default: built-in BGE-M3 via ONNX Runtime (no API key needed).
// Users can switch between builtin models (bge-m3, minilm) or external providers.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useState } from "react";
import {
  Cpu,
  Cloud,
  CheckCircle,
  Loader2,
  Link,
  Key,
  Eye,
  EyeOff,
  Zap,
  CreditCard,
  AlertTriangle,
  XCircle,
} from "lucide-react";
import { useEmbeddingsConfig } from "@/lib/api/queries";
import { useUpdateEmbeddings, useTestEmbedding, useCheckEmbeddingCredits } from "@/lib/api/mutations";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import type { ExternalEmbeddingProvider, EmbeddingModelOption, BuiltinEmbeddingModel } from "@/types/api";

export function EmbeddingsSettings() {
  const { data: config, isLoading } = useEmbeddingsConfig();
  const updateEmbeddings = useUpdateEmbeddings();
  const testEmbedding = useTestEmbedding();
  const checkCredits = useCheckEmbeddingCredits();

  const [useBuiltin, setUseBuiltin] = useState(true);
  const [selectedBuiltinModel, setSelectedBuiltinModel] = useState("bge-m3");
  const [selectedProvider, setSelectedProvider] = useState<string>("");
  // Per-provider endpoint URLs (pre-filled from backend, editable by user).
  const [endpoints, setEndpoints] = useState<Record<string, string>>({});
  // Per-provider selected model.
  const [selectedModels, setSelectedModels] = useState<Record<string, string>>({});
  // Dedicated embedding API key (separate from LLM key).
  const [embeddingApiKey, setEmbeddingApiKey] = useState("");
  const [showApiKey, setShowApiKey] = useState(false);

  // Sync state from backend on load.
  useEffect(() => {
    if (config) {
      setUseBuiltin(config.use_builtin);
      setSelectedBuiltinModel(config.active_builtin_model || "bge-m3");
      setSelectedProvider(config.active_provider || "");
      const eps: Record<string, string> = {};
      const models: Record<string, string> = {};
      for (const ep of config.external_providers ?? []) {
        eps[ep.name] = ep.endpoint;
        models[ep.name] = ep.model;
      }
      setEndpoints(eps);
      setSelectedModels(models);
    }
  }, [config]);

  const handleToggleBuiltin = (checked: boolean) => {
    setUseBuiltin(checked);
    if (checked) {
      updateEmbeddings.mutate({
        use_builtin: true,
        builtin_model: selectedBuiltinModel,
      });
      setSelectedProvider("");
      setEmbeddingApiKey("");
    }
  };

  const handleSelectBuiltinModel = (name: string) => {
    setSelectedBuiltinModel(name);
    updateEmbeddings.mutate({
      use_builtin: true,
      builtin_model: name,
    });
  };

  const handleSelectProvider = (providerName: string) => {
    setSelectedProvider(providerName);
    setEmbeddingApiKey("");
    testEmbedding.reset();
    checkCredits.reset();
  };

  const getSelectedModel = (provider: ExternalEmbeddingProvider): EmbeddingModelOption | undefined => {
    const modelName = selectedModels[provider.name] || provider.model;
    return provider.available_models?.find((m) => m.name === modelName);
  };

  const handleModelChange = (providerName: string, modelName: string) => {
    setSelectedModels((prev) => ({ ...prev, [providerName]: modelName }));
  };

  const handleSaveProvider = () => {
    const ep = config?.external_providers?.find((p) => p.name === selectedProvider);
    if (!ep) return;
    const model = getSelectedModel(ep);
    updateEmbeddings.mutate({
      use_builtin: false,
      provider: ep.name,
      endpoint: endpoints[ep.name] || ep.endpoint,
      model: selectedModels[ep.name] || ep.model,
      dimensions: model?.dimensions ?? ep.dimensions,
      ...(embeddingApiKey ? { api_key: embeddingApiKey } : {}),
    });
  };

  const handleTest = () => {
    const ep = config?.external_providers?.find((p) => p.name === selectedProvider);
    if (!ep) return;
    testEmbedding.mutate({
      provider: ep.name,
      endpoint: endpoints[ep.name] || ep.endpoint,
      model: selectedModels[ep.name] || ep.model,
      ...(embeddingApiKey ? { api_key: embeddingApiKey } : {}),
    });
  };

  const handleCheckCredits = () => {
    const ep = config?.external_providers?.find((p) => p.name === selectedProvider);
    if (!ep) return;
    checkCredits.mutate({
      provider: ep.name,
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
                    Use built-in local model
                  </Label>
                  <Badge variant="success" className="text-xs">
                    <CheckCircle className="mr-1 h-3 w-3" />
                    Recommended
                  </Badge>
                </div>
                <p className="text-xs text-muted-foreground ml-6">
                  Runs entirely on your server via ONNX Runtime — no API key required, no data leaves your infrastructure.
                </p>
              </div>
              <Switch
                id="builtin-toggle"
                checked={useBuiltin}
                onCheckedChange={handleToggleBuiltin}
                disabled={updateEmbeddings.isPending}
              />
            </div>

            {/* Builtin model selector */}
            {useBuiltin && (
              <div className="space-y-3">
                <p className="text-sm font-medium">Select Model</p>
                <div className="grid gap-2">
                  {(config?.builtin_models ?? []).map((m: BuiltinEmbeddingModel) => {
                    const isSelected = selectedBuiltinModel === m.name;
                    return (
                      <div
                        key={m.name}
                        className={`flex items-start gap-3 rounded-lg border p-3 cursor-pointer transition-colors ${
                          isSelected ? "border-primary bg-primary/5" : "hover:border-muted-foreground/30"
                        }`}
                        onClick={() => handleSelectBuiltinModel(m.name)}
                        role="button"
                        tabIndex={0}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" || e.key === " ") handleSelectBuiltinModel(m.name);
                        }}
                      >
                        <div className="mt-0.5">
                          <div className={`h-4 w-4 rounded-full border-2 flex items-center justify-center ${
                            isSelected ? "border-primary" : "border-muted-foreground/40"
                          }`}>
                            {isSelected && <div className="h-2 w-2 rounded-full bg-primary" />}
                          </div>
                        </div>
                        <div className="flex-1 space-y-1">
                          <div className="flex items-center gap-2">
                            <span className="text-sm font-medium">{m.display_name}</span>
                            <Badge variant="outline" className="text-[10px]">{m.dimensions}d</Badge>
                            {m.name === "bge-m3" && (
                              <Badge variant="success" className="text-[10px]">Best Quality</Badge>
                            )}
                            {m.name === "minilm" && (
                              <Badge variant="secondary" className="text-[10px]">Lightweight</Badge>
                            )}
                          </div>
                          <p className="text-xs text-muted-foreground">{m.description}</p>
                          <div className="flex gap-3 text-[10px] text-muted-foreground">
                            <span>Provider: {m.provider}</span>
                            <span>Size: {m.size_mb >= 1000 ? `${(m.size_mb / 1000).toFixed(1)} GB` : `${m.size_mb} MB`}</span>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
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
                  Select a provider, choose a model, verify the endpoint URL, and
                  provide your API key to activate.
                </p>

                {/* Provider cards */}
                <div className="space-y-2">
                  {(config?.external_providers ?? []).map((ep) => {
                    const isActive = selectedProvider === ep.name;
                    const currentModel = getSelectedModel(ep);

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
                              <span>Model: {selectedModels[ep.name] || ep.model}</span>
                              <span>Dimensions: {currentModel?.dimensions ?? ep.dimensions}</span>
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            <Badge
                              variant={ep.configured ? "success" : "secondary"}
                              className="text-xs"
                            >
                              {ep.configured ? "API Key Set" : ep.requires_key ? "No API Key" : "No Key Needed"}
                            </Badge>
                          </div>
                        </div>

                        {/* Expanded config panel for selected provider */}
                        {isActive && (
                          <div className="rounded-b-lg border border-t-0 border-primary bg-primary/5 p-4 space-y-4">
                            {/* Model selection */}
                            {ep.available_models && ep.available_models.length > 0 && (
                              <div className="space-y-1.5">
                                <Label className="text-xs font-medium flex items-center gap-1.5">
                                  <Zap className="h-3.5 w-3.5" />
                                  Model
                                </Label>
                                <Select
                                  value={selectedModels[ep.name] || ep.model}
                                  onValueChange={(value) => handleModelChange(ep.name, value)}
                                >
                                  <SelectTrigger className="h-8 text-xs">
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    {ep.available_models.map((m) => (
                                      <SelectItem key={m.name} value={m.name}>
                                        <div className="flex items-center gap-2">
                                          <span className="font-mono">{m.name}</span>
                                          <span className="text-muted-foreground">
                                            ({m.dimensions}d)
                                          </span>
                                        </div>
                                      </SelectItem>
                                    ))}
                                  </SelectContent>
                                </Select>
                                {currentModel && (
                                  <p className="text-[10px] text-muted-foreground">
                                    {currentModel.description} · {currentModel.dimensions} dimensions
                                  </p>
                                )}
                              </div>
                            )}

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
                                {ep.name === "custom"
                                  ? "Enter the URL of your self-hosted embedding server (Ollama, vLLM, TEI, etc.)."
                                  : "Pre-filled with the standard endpoint. Edit only if you use a proxy or custom deployment."}
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
                                      (leave blank to use saved key)
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
                                        ? "Using saved key (override here)"
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
                                <p className="text-[10px] text-muted-foreground">
                                  API keys are stored in an encrypted user-scoped vault on the server. They are never
                                  persisted in the C++ engine.
                                </p>
                                {!ep.configured && !embeddingApiKey && (
                                  <p className="text-[10px] text-amber-600 dark:text-amber-400">
                                    No API key found. Please enter one above.
                                  </p>
                                )}
                              </div>
                            )}

                            {/* Action buttons row */}
                            <div className="flex gap-2">
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={handleTest}
                                disabled={testEmbedding.isPending}
                                className="flex-1"
                              >
                                {testEmbedding.isPending ? (
                                  <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
                                ) : (
                                  <Zap className="mr-2 h-3.5 w-3.5" />
                                )}
                                Test Connection
                              </Button>

                              {ep.requires_key && (
                                <Button
                                  size="sm"
                                  variant="outline"
                                  onClick={handleCheckCredits}
                                  disabled={checkCredits.isPending}
                                  className="flex-1"
                                >
                                  {checkCredits.isPending ? (
                                    <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
                                  ) : (
                                    <CreditCard className="mr-2 h-3.5 w-3.5" />
                                  )}
                                  Check Credits
                                </Button>
                              )}
                            </div>

                            {/* Test result feedback */}
                            {testEmbedding.data && testEmbedding.data.provider === ep.name && (
                              <div
                                className={`flex items-center gap-2 rounded-md p-2 text-xs ${
                                  testEmbedding.data.healthy
                                    ? "bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300"
                                    : "bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300"
                                }`}
                              >
                                {testEmbedding.data.healthy ? (
                                  <>
                                    <CheckCircle className="h-3.5 w-3.5" />
                                    Connected successfully
                                    {testEmbedding.data.dimensions
                                      ? ` · ${testEmbedding.data.dimensions} dimensions`
                                      : ""}
                                  </>
                                ) : (
                                  <>
                                    <XCircle className="h-3.5 w-3.5" />
                                    {testEmbedding.data.error || "Connection failed"}
                                  </>
                                )}
                              </div>
                            )}

                            {/* Credits result feedback */}
                            {checkCredits.data && checkCredits.data.provider === ep.name && (
                              <div
                                className={`flex items-center gap-2 rounded-md p-2 text-xs ${
                                  checkCredits.data.status === "ok"
                                    ? "bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300"
                                    : checkCredits.data.status === "low_balance"
                                      ? "bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-300"
                                      : "bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300"
                                }`}
                              >
                                {checkCredits.data.status === "ok" ? (
                                  <CheckCircle className="h-3.5 w-3.5" />
                                ) : checkCredits.data.status === "low_balance" ? (
                                  <AlertTriangle className="h-3.5 w-3.5" />
                                ) : (
                                  <XCircle className="h-3.5 w-3.5" />
                                )}
                                {checkCredits.data.message || checkCredits.data.status}
                              </div>
                            )}

                            {/* Save button */}
                            <Button
                              size="sm"
                              onClick={handleSaveProvider}
                              disabled={
                                updateEmbeddings.isPending ||
                                (ep.requires_key && !ep.configured && !embeddingApiKey)
                              }
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
