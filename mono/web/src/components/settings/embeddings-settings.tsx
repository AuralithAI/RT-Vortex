// ─── Embeddings Settings ─────────────────────────────────────────────────────
// Unified multimodal embedding configuration.
// Users can enable/disable per-modality embedding for code/text, images, audio.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useState } from "react";
import * as SwitchPrimitives from "@radix-ui/react-switch";
import { motion, AnimatePresence } from "framer-motion";
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
  FileText,
  Image as ImageIcon,
  Mic,
  Settings2,
} from "lucide-react";
import { useEmbeddingsConfig, useMultimodalConfig } from "@/lib/api/queries";
import { useUpdateEmbeddings, useTestEmbedding, useCheckEmbeddingCredits, useUpdateMultimodal } from "@/lib/api/mutations";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import type { ExternalEmbeddingProvider, EmbeddingModelOption, BuiltinEmbeddingModel, ModalityInfo } from "@/types/api";

// ── Fallback data ───────────────────────────────────────────────────────────

const FALLBACK_BUILTIN_MODELS: BuiltinEmbeddingModel[] = [
  {
    name: "bge-m3",
    display_name: "BAAI BGE-M3",
    provider: "Built-in",
    dimensions: 1024,
    size_mb: 2200,
    description:
      "State-of-the-art multilingual embedding model. Best quality for semantic code search across 100+ languages.",
  },
  {
    name: "minilm",
    display_name: "MiniLM-L6",
    provider: "Built-in",
    dimensions: 384,
    size_mb: 80,
    description:
      "Lightweight, fast embedding model. Lower quality but 25× smaller. Good for quick evaluation or resource-constrained environments.",
  },
];

// ── Tab definitions ─────────────────────────────────────────────────────────

type TabKey = "text" | "image" | "audio";

interface TabDef {
  key: TabKey;
  label: string;
  icon: typeof FileText;
  description: string;
}

const TABS: TabDef[] = [
  {
    key: "text",
    label: "Code & Text",
    icon: FileText,
    description: "Powers semantic search across your codebase, documentation, and text files.",
  },
  {
    key: "image",
    label: "Images",
    icon: ImageIcon,
    description: "Search screenshots, diagrams, mockups, and visual assets alongside your code.",
  },
  {
    key: "audio",
    label: "Audio",
    icon: Mic,
    description: "Find voice recordings, meeting notes, and audio assets in your project.",
  },
];

// ── Component ───────────────────────────────────────────────────────────────

export function EmbeddingsSettings() {
  const { data: config, isLoading } = useEmbeddingsConfig();
  const { data: multimodal, isLoading: mmLoading } = useMultimodalConfig();
  const updateEmbeddings = useUpdateEmbeddings();
  const updateMultimodal = useUpdateMultimodal();
  const testEmbedding = useTestEmbedding();
  const checkCredits = useCheckEmbeddingCredits();

  const [activeTab, setActiveTab] = useState<TabKey>("text");
  const [useBuiltin, setUseBuiltin] = useState(true);
  const [selectedBuiltinModel, setSelectedBuiltinModel] = useState("bge-m3");
  const [selectedProvider, setSelectedProvider] = useState<string>("");
  const [endpoints, setEndpoints] = useState<Record<string, string>>({});
  const [selectedModels, setSelectedModels] = useState<Record<string, string>>({});
  const [embeddingApiKey, setEmbeddingApiKey] = useState("");
  const [showApiKey, setShowApiKey] = useState(false);

  // Multimodal toggles.
  const [imageEnabled, setImageEnabled] = useState(true);
  const [audioEnabled, setAudioEnabled] = useState(true);

  // Sync state from backend.
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

  useEffect(() => {
    if (multimodal) {
      setImageEnabled(multimodal.image_enabled);
      setAudioEnabled(multimodal.audio_enabled);
    }
  }, [multimodal]);

  // ── Text model handlers ─────────────────────────────────────────────────

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

  // ── Multimodal handlers ─────────────────────────────────────────────────

  const handleToggleImage = (checked: boolean) => {
    setImageEnabled(checked);
    updateMultimodal.mutate({ image_enabled: checked });
  };

  const handleToggleAudio = (checked: boolean) => {
    setAudioEnabled(checked);
    updateMultimodal.mutate({ audio_enabled: checked });
  };

  // ── Helpers ─────────────────────────────────────────────────────────────

  const getModalityInfo = (modality: string): ModalityInfo | undefined =>
    multimodal?.modalities?.find((m) => m.modality === modality);

  const formatSize = (mb: number) =>
    mb >= 1000 ? `${(mb / 1000).toFixed(1)} GB` : `${mb} MB`;

  const getStatusBadge = (status: string) => {
    switch (status) {
      case "ready":
        return <Badge variant="success" className="text-[10px]"><CheckCircle className="mr-1 h-2.5 w-2.5" />Ready</Badge>;
      case "downloading":
        return <Badge variant="default" className="text-[10px]"><Loader2 className="mr-1 h-2.5 w-2.5 animate-spin" />Downloading</Badge>;
      case "pending":
        return <Badge variant="secondary" className="text-[10px]">Pending</Badge>;
      default:
        return <Badge variant="destructive" className="text-[10px]"><XCircle className="mr-1 h-2.5 w-2.5" />Error</Badge>;
    }
  };

  // ── Render ──────────────────────────────────────────────────────────────

  return (
    <Card className="max-w-3xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Settings2 className="h-5 w-5" />
          Search & Embeddings
        </CardTitle>
        <CardDescription>
          Configure how your codebase, documents, images, and audio are indexed
          for semantic search.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {(isLoading || mmLoading) ? (
          <Skeleton className="h-40 w-full" />
        ) : (
          <>
            {/* ── Tab bar ────────────────────────────────────────────── */}
            <div className="flex gap-1 rounded-lg bg-muted/50 p-1">
              {TABS.map((tab) => {
                const isActive = activeTab === tab.key;
                const Icon = tab.icon;
                const info = getModalityInfo(tab.key);
                const isEnabled = tab.key === "text" ? true : tab.key === "image" ? imageEnabled : audioEnabled;
                return (
                  <button
                    key={tab.key}
                    onClick={() => setActiveTab(tab.key)}
                    className={`flex-1 flex items-center justify-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-all ${
                      isActive
                        ? "bg-background shadow-sm text-foreground"
                        : "text-muted-foreground hover:text-foreground"
                    }`}
                  >
                    <Icon className="h-4 w-4" />
                    <span className="hidden sm:inline">{tab.label}</span>
                    {!isEnabled && tab.key !== "text" && (
                      <span className="h-1.5 w-1.5 rounded-full bg-muted-foreground/40" />
                    )}
                    {isEnabled && info?.status === "ready" && tab.key !== "text" && (
                      <span className="h-1.5 w-1.5 rounded-full bg-green-500" />
                    )}
                  </button>
                );
              })}
            </div>

            {/* ── Tab content ────────────────────────────────────────── */}
            <AnimatePresence mode="wait">
              <motion.div
                key={activeTab}
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -8 }}
                transition={{ duration: 0.2 }}
                className="space-y-4"
              >
                {activeTab === "text" && (
                  <TextTab
                    useBuiltin={useBuiltin}
                    selectedBuiltinModel={selectedBuiltinModel}
                    config={config}
                    selectedProvider={selectedProvider}
                    endpoints={endpoints}
                    selectedModels={selectedModels}
                    embeddingApiKey={embeddingApiKey}
                    showApiKey={showApiKey}
                    updatePending={updateEmbeddings.isPending}
                    testResult={testEmbedding.data}
                    testPending={testEmbedding.isPending}
                    creditsResult={checkCredits.data}
                    creditsPending={checkCredits.isPending}
                    updateSuccess={updateEmbeddings.isSuccess}
                    onToggleBuiltin={handleToggleBuiltin}
                    onSelectBuiltinModel={handleSelectBuiltinModel}
                    onSelectProvider={handleSelectProvider}
                    onModelChange={handleModelChange}
                    onEndpointChange={(name, val) =>
                      setEndpoints((prev) => ({ ...prev, [name]: val }))
                    }
                    onApiKeyChange={setEmbeddingApiKey}
                    onToggleShowApiKey={() => setShowApiKey(!showApiKey)}
                    onSaveProvider={handleSaveProvider}
                    onTest={handleTest}
                    onCheckCredits={handleCheckCredits}
                    getSelectedModel={getSelectedModel}
                  />
                )}

                {activeTab === "image" && (
                  <ModalityTab
                    modality="image"
                    enabled={imageEnabled}
                    onToggle={handleToggleImage}
                    info={getModalityInfo("image")}
                    description={TABS[1].description}
                    icon={ImageIcon}
                    updatePending={updateMultimodal.isPending}
                    formatSize={formatSize}
                    getStatusBadge={getStatusBadge}
                  />
                )}

                {activeTab === "audio" && (
                  <ModalityTab
                    modality="audio"
                    enabled={audioEnabled}
                    onToggle={handleToggleAudio}
                    info={getModalityInfo("audio")}
                    description={TABS[2].description}
                    icon={Mic}
                    updatePending={updateMultimodal.isPending}
                    formatSize={formatSize}
                    getStatusBadge={getStatusBadge}
                  />
                )}
              </motion.div>
            </AnimatePresence>
          </>
        )}
      </CardContent>
    </Card>
  );
}

// ── Text Tab (code & text embeddings) ───────────────────────────────────────

interface TextTabProps {
  useBuiltin: boolean;
  selectedBuiltinModel: string;
  config: any;
  selectedProvider: string;
  endpoints: Record<string, string>;
  selectedModels: Record<string, string>;
  embeddingApiKey: string;
  showApiKey: boolean;
  updatePending: boolean;
  testResult: any;
  testPending: boolean;
  creditsResult: any;
  creditsPending: boolean;
  updateSuccess: boolean;
  onToggleBuiltin: (checked: boolean) => void;
  onSelectBuiltinModel: (name: string) => void;
  onSelectProvider: (name: string) => void;
  onModelChange: (provider: string, model: string) => void;
  onEndpointChange: (provider: string, value: string) => void;
  onApiKeyChange: (key: string) => void;
  onToggleShowApiKey: () => void;
  onSaveProvider: () => void;
  onTest: () => void;
  onCheckCredits: () => void;
  getSelectedModel: (ep: ExternalEmbeddingProvider) => EmbeddingModelOption | undefined;
}

function TextTab({
  useBuiltin,
  selectedBuiltinModel,
  config,
  selectedProvider,
  endpoints,
  selectedModels,
  embeddingApiKey,
  showApiKey,
  updatePending,
  testResult,
  testPending,
  creditsResult,
  creditsPending,
  updateSuccess,
  onToggleBuiltin,
  onSelectBuiltinModel,
  onSelectProvider,
  onModelChange,
  onEndpointChange,
  onApiKeyChange,
  onToggleShowApiKey,
  onSaveProvider,
  onTest,
  onCheckCredits,
  getSelectedModel,
}: TextTabProps) {
  return (
    <div className="space-y-4">
      {/* Toggle: built-in vs external */}
      <div className="flex items-start justify-between rounded-lg border p-4">
        <div className="space-y-2 flex-1">
          <div className="flex items-center gap-2">
            {useBuiltin ? (
              <Cpu className="h-4 w-4 text-primary" />
            ) : (
              <Cloud className="h-4 w-4 text-primary" />
            )}
            <Label htmlFor="builtin-toggle" className="text-sm font-semibold">
              Use built-in model
            </Label>
            <Badge variant="success" className="text-xs">
              <CheckCircle className="mr-1 h-3 w-3" />
              Recommended
            </Badge>
          </div>
          <p className="text-xs text-muted-foreground ml-6">
            {useBuiltin
              ? "Runs entirely on your server — no API key required, no data leaves your infrastructure."
              : "Using an external cloud provider. Switch back to run locally."}
          </p>
        </div>
        <SwitchPrimitives.Root
          id="builtin-toggle"
          checked={useBuiltin}
          onCheckedChange={onToggleBuiltin}
          disabled={updatePending}
          className="group relative inline-flex h-8 w-14 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent transition-colors duration-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-indigo-600 data-[state=unchecked]:bg-zinc-300 dark:data-[state=unchecked]:bg-zinc-600 dark:data-[state=checked]:bg-indigo-500"
        >
          <span className="pointer-events-none absolute inset-0 rounded-full opacity-0 transition-opacity group-hover:opacity-100 group-hover:shadow-[0_0_8px_2px_rgba(99,102,241,0.25)] dark:group-hover:shadow-[0_0_8px_2px_rgba(129,140,248,0.3)]" />
          <SwitchPrimitives.Thumb asChild>
            <motion.span
              layout
              transition={{ type: "spring", stiffness: 500, damping: 30 }}
              className="pointer-events-none flex h-6 w-6 items-center justify-center rounded-full bg-white shadow-lg data-[state=unchecked]:translate-x-0.5 data-[state=checked]:translate-x-[1.625rem]"
            >
              <motion.span
                animate={{ rotate: useBuiltin ? 0 : 180 }}
                transition={{ duration: 0.4, ease: "easeInOut" }}
                className="flex items-center justify-center"
              >
                {useBuiltin ? (
                  <Cpu className="h-3.5 w-3.5 text-indigo-600" />
                ) : (
                  <Cloud className="h-3.5 w-3.5 text-zinc-500" />
                )}
              </motion.span>
            </motion.span>
          </SwitchPrimitives.Thumb>
        </SwitchPrimitives.Root>
      </div>

      {/* Built-in model selector */}
      {useBuiltin && (
        <motion.div
          initial={{ opacity: 0, height: 0 }}
          animate={{ opacity: 1, height: "auto" }}
          exit={{ opacity: 0, height: 0 }}
          transition={{ duration: 0.25, ease: "easeOut" }}
          className="space-y-3"
        >
          <p className="text-sm font-medium">Select Model</p>
          <div className="grid gap-2">
            {(config?.builtin_models?.length
              ? config.builtin_models
              : FALLBACK_BUILTIN_MODELS
            ).map((m: BuiltinEmbeddingModel) => {
              const isSelected = selectedBuiltinModel === m.name;
              return (
                <motion.div
                  key={m.name}
                  whileHover={{ scale: 1.01 }}
                  whileTap={{ scale: 0.99 }}
                  className={`flex items-start gap-3 rounded-lg border-2 p-3 cursor-pointer transition-colors ${
                    isSelected
                      ? "border-indigo-500 bg-indigo-50 dark:bg-indigo-950/30 shadow-sm"
                      : "border-transparent bg-muted/40 hover:bg-muted/70"
                  }`}
                  onClick={() => onSelectBuiltinModel(m.name)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") onSelectBuiltinModel(m.name);
                  }}
                >
                  <div className="mt-0.5">
                    <div className={`h-5 w-5 rounded-full border-2 flex items-center justify-center transition-colors ${
                      isSelected ? "border-indigo-500 bg-indigo-500" : "border-muted-foreground/40"
                    }`}>
                      {isSelected && (
                        <motion.div
                          initial={{ scale: 0 }}
                          animate={{ scale: 1 }}
                          transition={{ type: "spring", stiffness: 500, damping: 30 }}
                        >
                          <CheckCircle className="h-3 w-3 text-white" />
                        </motion.div>
                      )}
                    </div>
                  </div>
                  <div className="flex-1 space-y-1">
                    <div className="flex items-center gap-2">
                      <span className={`text-sm font-medium ${isSelected ? "text-indigo-700 dark:text-indigo-300" : ""}`}>
                        {m.display_name}
                      </span>
                      {m.name === "bge-m3" && (
                        <Badge variant="success" className="text-[10px]">Best Quality</Badge>
                      )}
                      {m.name === "minilm" && (
                        <Badge variant="secondary" className="text-[10px]">Lightweight</Badge>
                      )}
                    </div>
                    <p className="text-xs text-muted-foreground">{m.description}</p>
                    <div className="flex gap-3 text-[10px] text-muted-foreground">
                      <span>Size: {m.size_mb >= 1000 ? `${(m.size_mb / 1000).toFixed(1)} GB` : `${m.size_mb} MB`}</span>
                    </div>
                  </div>
                </motion.div>
              );
            })}
          </div>
        </motion.div>
      )}

      {/* External providers */}
      {!useBuiltin && (
        <div className="space-y-4">
          <p className="text-sm font-medium flex items-center gap-2">
            <Cloud className="h-4 w-4" />
            External Embedding Providers
          </p>
          <p className="text-xs text-muted-foreground">
            Use a cloud embedding API instead of the built-in model.
            Select a provider, choose a model, and provide your API key.
          </p>

          <div className="space-y-2">
            {(config?.external_providers ?? []).map((ep: ExternalEmbeddingProvider) => {
              const isActive = selectedProvider === ep.name;
              const currentModel = getSelectedModel(ep);
              return (
                <div key={ep.name} className="space-y-0">
                  <div
                    className={`flex items-center justify-between rounded-lg border p-3 cursor-pointer transition-colors ${
                      isActive ? "border-primary bg-primary/5 rounded-b-none" : "hover:border-muted-foreground/30"
                    }`}
                    onClick={() => onSelectProvider(ep.name)}
                    role="button"
                    tabIndex={0}
                    onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") onSelectProvider(ep.name); }}
                  >
                    <div className="space-y-0.5">
                      <div className="flex items-center gap-2">
                        <p className="text-sm font-medium">{ep.display_name}</p>
                        {isActive && config?.active_provider === ep.name && (
                          <Badge variant="default" className="text-xs">Active</Badge>
                        )}
                      </div>
                      <div className="flex gap-3 text-xs text-muted-foreground">
                        <span>Model: {selectedModels[ep.name] || ep.model}</span>
                      </div>
                    </div>
                    <Badge variant={ep.configured ? "success" : "secondary"} className="text-xs">
                      {ep.configured ? "API Key Set" : ep.requires_key ? "No API Key" : "No Key Needed"}
                    </Badge>
                  </div>

                  {isActive && (
                    <div className="rounded-b-lg border border-t-0 border-primary bg-primary/5 p-4 space-y-4">
                      {ep.available_models && ep.available_models.length > 0 && (
                        <div className="space-y-1.5">
                          <Label className="text-xs font-medium flex items-center gap-1.5">
                            <Zap className="h-3.5 w-3.5" /> Model
                          </Label>
                          <Select
                            value={selectedModels[ep.name] || ep.model}
                            onValueChange={(value) => onModelChange(ep.name, value)}
                          >
                            <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                            <SelectContent>
                              {ep.available_models.map((m) => (
                                <SelectItem key={m.name} value={m.name}>
                                  <span className="font-mono">{m.name}</span>
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                          {currentModel && (
                            <p className="text-[10px] text-muted-foreground">{currentModel.description}</p>
                          )}
                        </div>
                      )}

                      <div className="space-y-1.5">
                        <Label htmlFor={`endpoint-${ep.name}`} className="text-xs font-medium flex items-center gap-1.5">
                          <Link className="h-3.5 w-3.5" /> Endpoint URL
                        </Label>
                        <Input
                          id={`endpoint-${ep.name}`}
                          value={endpoints[ep.name] || ""}
                          onChange={(e) => onEndpointChange(ep.name, e.target.value)}
                          placeholder={ep.endpoint}
                          className="font-mono text-xs h-8"
                        />
                      </div>

                      {ep.requires_key && (
                        <div className="space-y-1.5">
                          <Label htmlFor={`apikey-${ep.name}`} className="text-xs font-medium flex items-center gap-1.5">
                            <Key className="h-3.5 w-3.5" /> API Key
                            {ep.configured && (
                              <span className="text-[10px] text-muted-foreground font-normal">(leave blank to use saved key)</span>
                            )}
                          </Label>
                          <div className="relative">
                            <Input
                              id={`apikey-${ep.name}`}
                              type={showApiKey ? "text" : "password"}
                              value={embeddingApiKey}
                              onChange={(e) => onApiKeyChange(e.target.value)}
                              placeholder={ep.configured ? "Using saved key" : "Enter your API key"}
                              className="font-mono text-xs h-8 pr-9"
                            />
                            <button
                              type="button"
                              onClick={onToggleShowApiKey}
                              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                            >
                              {showApiKey ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                            </button>
                          </div>
                        </div>
                      )}

                      <div className="flex gap-2">
                        <Button size="sm" variant="outline" onClick={onTest} disabled={testPending} className="flex-1">
                          {testPending ? <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> : <Zap className="mr-2 h-3.5 w-3.5" />}
                          Test Connection
                        </Button>
                        {ep.requires_key && (
                          <Button size="sm" variant="outline" onClick={onCheckCredits} disabled={creditsPending} className="flex-1">
                            {creditsPending ? <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> : <CreditCard className="mr-2 h-3.5 w-3.5" />}
                            Check Credits
                          </Button>
                        )}
                      </div>

                      {testResult && testResult.provider === ep.name && (
                        <div className={`flex items-center gap-2 rounded-md p-2 text-xs ${
                          testResult.healthy
                            ? "bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300"
                            : "bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300"
                        }`}>
                          {testResult.healthy ? <><CheckCircle className="h-3.5 w-3.5" /> Connected successfully</> : <><XCircle className="h-3.5 w-3.5" /> {testResult.error || "Connection failed"}</>}
                        </div>
                      )}

                      {creditsResult && creditsResult.provider === ep.name && (
                        <div className={`flex items-center gap-2 rounded-md p-2 text-xs ${
                          creditsResult.status === "ok"
                            ? "bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300"
                            : creditsResult.status === "low_balance"
                              ? "bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-300"
                              : "bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300"
                        }`}>
                          {creditsResult.status === "ok" ? <CheckCircle className="h-3.5 w-3.5" /> : creditsResult.status === "low_balance" ? <AlertTriangle className="h-3.5 w-3.5" /> : <XCircle className="h-3.5 w-3.5" />}
                          {creditsResult.message || creditsResult.status}
                        </div>
                      )}

                      <Button
                        size="sm"
                        onClick={onSaveProvider}
                        disabled={updatePending || (ep.requires_key && !ep.configured && !embeddingApiKey)}
                        className="w-full"
                      >
                        {updatePending ? <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> : <CheckCircle className="mr-2 h-3.5 w-3.5" />}
                        Save &amp; Activate {ep.display_name}
                      </Button>
                    </div>
                  )}
                </div>
              );
            })}
          </div>

          {updateSuccess && !useBuiltin && (
            <div className="flex items-center gap-2 rounded-md bg-green-50 p-2 dark:bg-green-950 text-xs text-green-700 dark:text-green-300">
              <CheckCircle className="h-3.5 w-3.5" />
              Provider saved. It will be used for the next indexing run.
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ── Image / Audio modality tab ──────────────────────────────────────────────

interface ModalityTabProps {
  modality: string;
  enabled: boolean;
  onToggle: (checked: boolean) => void;
  info?: ModalityInfo;
  description: string;
  icon: typeof ImageIcon;
  updatePending: boolean;
  formatSize: (mb: number) => string;
  getStatusBadge: (status: string) => React.ReactNode;
}

function ModalityTab({
  modality,
  enabled,
  onToggle,
  info,
  description,
  icon: Icon,
  updatePending,
  formatSize,
  getStatusBadge,
}: ModalityTabProps) {
  return (
    <div className="space-y-4">
      {/* Enable / disable toggle */}
      <div className="flex items-start justify-between rounded-lg border p-4">
        <div className="space-y-2 flex-1">
          <div className="flex items-center gap-2">
            <Icon className="h-4 w-4 text-primary" />
            <Label htmlFor={`${modality}-toggle`} className="text-sm font-semibold">
              Enable {modality === "image" ? "Image" : "Audio"} Search
            </Label>
            {info && getStatusBadge(info.status)}
          </div>
          <p className="text-xs text-muted-foreground ml-6">
            {description}
          </p>
        </div>
        <SwitchPrimitives.Root
          id={`${modality}-toggle`}
          checked={enabled}
          onCheckedChange={onToggle}
          disabled={updatePending}
          className="group relative inline-flex h-8 w-14 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent transition-colors duration-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-indigo-600 data-[state=unchecked]:bg-zinc-300 dark:data-[state=unchecked]:bg-zinc-600 dark:data-[state=checked]:bg-indigo-500"
        >
          <span className="pointer-events-none absolute inset-0 rounded-full opacity-0 transition-opacity group-hover:opacity-100 group-hover:shadow-[0_0_8px_2px_rgba(99,102,241,0.25)] dark:group-hover:shadow-[0_0_8px_2px_rgba(129,140,248,0.3)]" />
          <SwitchPrimitives.Thumb asChild>
            <motion.span
              layout
              transition={{ type: "spring", stiffness: 500, damping: 30 }}
              className="pointer-events-none flex h-6 w-6 items-center justify-center rounded-full bg-white shadow-lg data-[state=unchecked]:translate-x-0.5 data-[state=checked]:translate-x-[1.625rem]"
            >
              <Icon className={`h-3.5 w-3.5 ${enabled ? "text-indigo-600" : "text-zinc-500"}`} />
            </motion.span>
          </SwitchPrimitives.Thumb>
        </SwitchPrimitives.Root>
      </div>

      {/* Model info card */}
      {enabled && info && (
        <motion.div
          initial={{ opacity: 0, height: 0 }}
          animate={{ opacity: 1, height: "auto" }}
          exit={{ opacity: 0, height: 0 }}
          transition={{ duration: 0.25, ease: "easeOut" }}
        >
          <div className="rounded-lg border bg-muted/30 p-4 space-y-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{info.model_name}</span>
                {getStatusBadge(info.status)}
              </div>
              <span className="text-xs text-muted-foreground">
                {formatSize(info.size_mb)}
              </span>
            </div>

            <p className="text-xs text-muted-foreground">
              {info.description}
            </p>

            {info.status === "downloading" && info.download_progress > 0 && (
              <div className="space-y-1">
                <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
                  <motion.div
                    className="h-full rounded-full bg-indigo-500"
                    initial={{ width: 0 }}
                    animate={{ width: `${info.download_progress}%` }}
                    transition={{ duration: 0.5 }}
                  />
                </div>
                <p className="text-[10px] text-muted-foreground text-right">
                  {info.download_progress}%
                </p>
              </div>
            )}

            <div className="flex items-center gap-4 pt-1 text-[10px] text-muted-foreground border-t border-muted">
              <span>Supported formats: {modality === "image" ? "PNG, JPEG, WebP, SVG" : "WAV, MP3, OGG, FLAC"}</span>
            </div>
          </div>
        </motion.div>
      )}

      {/* Disabled info */}
      {!enabled && (
        <div className="rounded-lg border border-dashed p-4 text-center">
          <p className="text-xs text-muted-foreground">
            {modality === "image" ? "Image" : "Audio"} search is disabled. Enable it above
            to index and search {modality === "image" ? "screenshots, diagrams, and visual assets" : "recordings, voice notes, and audio files"}.
          </p>
        </div>
      )}
    </div>
  );
}
