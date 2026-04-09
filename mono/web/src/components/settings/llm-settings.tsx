// ─── LLM Settings ────────────────────────────────────────────────────────────
// Full LLM provider configuration: API keys, model selection, health testing.
// For Ollama/local: shows URL input + Test Connection button.
// For cloud providers: checks credit balance and shows warning if low.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useState } from "react";
import {
  CheckCircle,
  XCircle,
  Loader2,
  Zap,
  Eye,
  EyeOff,
  Star,
  KeyRound,
  Globe,
  AlertTriangle,
  Link,
  Cpu,
  HardDrive,
  Server,
  MonitorDot,
} from "lucide-react";
import { useLLMProviders, useLLMProviderStatus } from "@/lib/api/queries";
import {
  useTestLLM,
  useConfigureLLM,
  useSetPrimaryLLM,
  useCheckLLMBalance,
} from "@/lib/api/mutations";
import type { LLMBalanceResult } from "@/types/api";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import { getLLMIcon } from "@/components/icons/brand-icons";

/** Format bytes into human-readable size (e.g. 4.1 GB) */
function formatBytes(bytes: number): string {
  if (!bytes || bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const val = bytes / Math.pow(1024, i);
  return `${val.toFixed(val < 10 ? 1 : 0)} ${units[i]}`;
}

export function LLMSettings() {
  const { data: llmData, isLoading } = useLLMProviders();
  const providers = llmData?.providers ?? [];
  const primaryProvider = llmData?.primary ?? "";
  const testLLM = useTestLLM();
  const configureLLM = useConfigureLLM();
  const setPrimaryLLM = useSetPrimaryLLM();
  const checkBalance = useCheckLLMBalance();

  const [apiKeys, setApiKeys] = useState<Record<string, string>>({});
  const [showKeys, setShowKeys] = useState<Record<string, boolean>>({});
  const [selectedModels, setSelectedModels] = useState<Record<string, string>>({});
  const [baseUrls, setBaseUrls] = useState<Record<string, string>>({});
  const [balanceResults, setBalanceResults] = useState<Record<string, LLMBalanceResult>>({});

  // Fetch Ollama-specific live status (running models, detailed model info)
  const ollamaProvider = providers.find((p: any) => p.name === "ollama");
  const ollamaStatus = useLLMProviderStatus(
    "ollama",
    !!ollamaProvider?.configured
  );

  const handleSave = (providerName: string) => {
    const key = apiKeys[providerName];
    const model = selectedModels[providerName];
    const url = baseUrls[providerName];
    if (!key && !model && !url) return;
    configureLLM.mutate({
      provider: providerName,
      api_key: key || undefined,
      model: model || undefined,
      base_url: url || undefined,
    });
  };

  const handleTest = (providerName: string) => {
    testLLM.mutate({
      provider: providerName,
      api_key: apiKeys[providerName] || undefined,
      model: selectedModels[providerName] || undefined,
      base_url: baseUrls[providerName] || undefined,
    });
  };

  const handleCheckBalance = (providerName: string) => {
    checkBalance.mutate(providerName, {
      onSuccess: (result) => {
        setBalanceResults((prev) => ({ ...prev, [providerName]: result }));
      },
    });
  };

  return (
    <Card className="max-w-3xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Zap className="h-5 w-5" />
          LLM Providers
        </CardTitle>
        <CardDescription>
          Configure AI model providers for code review. All providers are available —
          just add your API key to enable each one. For local providers like Ollama,
          configure the URL and test the connection.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-32 w-full" />
            ))}
          </div>
        ) : !providers?.length ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            No LLM providers available. Check server configuration.
          </p>
        ) : (
          <div className="space-y-4">
            {providers.map((provider) => {
              const isPrimary = provider.name === primaryProvider;
              const testResult =
                testLLM.data && testLLM.variables?.provider === provider.name
                  ? testLLM.data
                  : null;
              const isConfiguring =
                configureLLM.isPending &&
                configureLLM.variables?.provider === provider.name;
              const isTesting =
                testLLM.isPending &&
                testLLM.variables?.provider === provider.name;
              const isCheckingBalance =
                checkBalance.isPending &&
                checkBalance.variables === provider.name;
              const balance = balanceResults[provider.name];
              const isLocal = !provider.requires_key; // Ollama, local providers
              const ProviderBrandIcon = getLLMIcon(provider.name);

              return (
                <div
                  key={provider.name}
                  className="rounded-lg border p-4 space-y-3"
                >
                  {/* Header row */}
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2.5">
                      {ProviderBrandIcon && (
                        <span className="flex h-6 w-6 shrink-0 items-center justify-center">
                          <ProviderBrandIcon size={22} />
                        </span>
                      )}
                      <p className="text-sm font-semibold">
                        {provider.display_name || provider.name}
                      </p>
                      {provider.configured ? (
                        <Badge
                          variant={provider.healthy ? "success" : "destructive"}
                          className="text-xs"
                        >
                          {provider.healthy ? "Connected" : "Configured"}
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="text-xs">
                          Not Configured
                        </Badge>
                      )}
                      {isPrimary && (
                        <Badge variant="default" className="text-xs">
                          <Star className="mr-1 h-3 w-3" />
                          Primary
                        </Badge>
                      )}
                    </div>
                    <div className="flex items-center gap-2">
                      {!isPrimary && provider.configured && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setPrimaryLLM.mutate(provider.name)}
                          disabled={setPrimaryLLM.isPending}
                        >
                          Set Primary
                        </Button>
                      )}

                      {/* Cloud providers: Check Balance button */}
                      {provider.requires_key && provider.configured && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => handleCheckBalance(provider.name)}
                          disabled={isCheckingBalance}
                        >
                          {isCheckingBalance ? (
                            <Loader2 className="mr-1 h-4 w-4 animate-spin" />
                          ) : null}
                          Check Balance
                        </Button>
                      )}

                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleTest(provider.name)}
                        disabled={isTesting}
                      >
                        {isTesting ? (
                          <Loader2 className="mr-1 h-4 w-4 animate-spin" />
                        ) : testResult ? (
                          testResult.healthy ? (
                            <CheckCircle className="mr-1 h-4 w-4 text-green-500" />
                          ) : (
                            <XCircle className="mr-1 h-4 w-4 text-red-500" />
                          )
                        ) : null}
                        {isLocal ? "Test Connection" : "Test"}
                      </Button>
                    </div>
                  </div>

                  {/* Info row */}
                  <div className="flex items-center gap-4 text-xs text-muted-foreground">
                    <span className="flex items-center gap-1">
                      <Globe className="h-3 w-3" />
                      {provider.base_url}
                    </span>
                    {isLocal && provider.name === "ollama" && ollamaStatus.data && (
                      <>
                        {ollamaStatus.data.models_detailed && (
                          <span className="flex items-center gap-1">
                            <HardDrive className="h-3 w-3" />
                            {ollamaStatus.data.models_detailed.length} model{ollamaStatus.data.models_detailed.length !== 1 ? "s" : ""} pulled
                          </span>
                        )}
                        {(ollamaStatus.data.running_count ?? 0) > 0 && (
                          <span className="flex items-center gap-1 text-green-600 dark:text-green-400">
                            <MonitorDot className="h-3 w-3" />
                            {ollamaStatus.data.running_count} running
                          </span>
                        )}
                      </>
                    )}
                  </div>

                  {/* Ollama live model status */}
                  {isLocal && provider.name === "ollama" && ollamaStatus.data && (
                    <div className="space-y-2">
                      {/* Running models */}
                      {ollamaStatus.data.running_models &&
                        ollamaStatus.data.running_models.length > 0 && (
                          <div className="rounded-md border border-green-200 bg-green-50/50 dark:border-green-900 dark:bg-green-950/30 p-2.5 space-y-1.5">
                            <div className="flex items-center gap-1.5 text-xs font-medium text-green-700 dark:text-green-400">
                              <MonitorDot className="h-3.5 w-3.5" />
                              Loaded in Memory ({ollamaStatus.data.running_count ?? ollamaStatus.data.running_models.length})
                            </div>
                            <div className="space-y-1">
                              {ollamaStatus.data.running_models.map((rm: any, idx: number) => (
                                <div
                                  key={rm.name + idx}
                                  className="flex items-center justify-between text-xs text-green-800 dark:text-green-300 rounded bg-green-100/60 dark:bg-green-900/40 px-2 py-1"
                                >
                                  <span className="font-mono font-medium">{rm.name}</span>
                                  <div className="flex items-center gap-3 text-[11px] text-green-600 dark:text-green-400">
                                    <TooltipProvider>
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <span className="flex items-center gap-0.5">
                                            <Cpu className="h-3 w-3" />
                                            {rm.processor || "cpu"}
                                          </span>
                                        </TooltipTrigger>
                                        <TooltipContent side="top" className="text-xs">Processor</TooltipContent>
                                      </Tooltip>
                                    </TooltipProvider>
                                    <TooltipProvider>
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <span className="flex items-center gap-0.5">
                                            <HardDrive className="h-3 w-3" />
                                            {formatBytes(rm.size)}
                                          </span>
                                        </TooltipTrigger>
                                        <TooltipContent side="top" className="text-xs">Total Size</TooltipContent>
                                      </Tooltip>
                                    </TooltipProvider>
                                    {rm.size_vram > 0 && (
                                      <TooltipProvider>
                                        <Tooltip>
                                          <TooltipTrigger asChild>
                                            <span className="flex items-center gap-0.5">
                                              <Server className="h-3 w-3" />
                                              {formatBytes(rm.size_vram)} VRAM
                                            </span>
                                          </TooltipTrigger>
                                          <TooltipContent side="top" className="text-xs">GPU VRAM Used</TooltipContent>
                                        </Tooltip>
                                      </TooltipProvider>
                                    )}
                                  </div>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}

                      {/* Available model details */}
                      {ollamaStatus.data.models_detailed &&
                        ollamaStatus.data.models_detailed.length > 0 && (
                          <div className="rounded-md border p-2.5 space-y-1.5">
                            <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                              <HardDrive className="h-3.5 w-3.5" />
                              Available Models ({ollamaStatus.data.models_detailed.length})
                            </div>
                            <div className="grid grid-cols-1 gap-1">
                              {ollamaStatus.data.models_detailed.map((md: any) => (
                                <div
                                  key={md.digest || md.name}
                                  className="flex items-center justify-between text-xs rounded px-2 py-1 bg-muted/40"
                                >
                                  <span className="font-mono font-medium truncate max-w-[200px]">{md.name}</span>
                                  <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
                                    {md.family && (
                                      <Badge variant="outline" className="text-[10px] h-4 px-1.5">
                                        {md.family}
                                      </Badge>
                                    )}
                                    {md.parameter_size && (
                                      <span className="tabular-nums">{md.parameter_size}</span>
                                    )}
                                    {md.quantization_level && (
                                      <span className="text-muted-foreground/70">{md.quantization_level}</span>
                                    )}
                                    <span className="tabular-nums">{formatBytes(md.size)}</span>
                                  </div>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}
                    </div>
                  )}

                  {/* Config inputs — different layout for local vs cloud */}
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    {/* Local providers (Ollama): URL input instead of API key */}
                    {isLocal && (
                      <div className="space-y-1.5">
                        <Label
                          htmlFor={`url-${provider.name}`}
                          className="text-xs flex items-center gap-1"
                        >
                          <Link className="h-3 w-3" />
                          Server URL
                        </Label>
                        <Input
                          id={`url-${provider.name}`}
                          type="url"
                          placeholder={provider.base_url || "http://localhost:11434"}
                          value={baseUrls[provider.name] ?? ""}
                          onChange={(e) =>
                            setBaseUrls((prev) => ({
                              ...prev,
                              [provider.name]: e.target.value,
                            }))
                          }
                          className="h-8 text-xs"
                        />
                      </div>
                    )}

                    {/* Cloud providers: API key input */}
                    {provider.requires_key && (
                      <div className="space-y-1.5">
                        <Label
                          htmlFor={`key-${provider.name}`}
                          className="text-xs flex items-center gap-1"
                        >
                          <KeyRound className="h-3 w-3" />
                          API Key
                        </Label>
                        <div className="flex gap-1">
                          <div className="relative flex-1">
                            <Input
                              id={`key-${provider.name}`}
                              type={showKeys[provider.name] ? "text" : "password"}
                              placeholder={
                                provider.configured
                                  ? "••••••••••••••••"
                                  : "Enter API key…"
                              }
                              value={apiKeys[provider.name] ?? ""}
                              onChange={(e) =>
                                setApiKeys((prev) => ({
                                  ...prev,
                                  [provider.name]: e.target.value,
                                }))
                              }
                              className="pr-8 text-xs h-8"
                            />
                            <button
                              type="button"
                              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                              onClick={() =>
                                setShowKeys((prev) => ({
                                  ...prev,
                                  [provider.name]: !prev[provider.name],
                                }))
                              }
                            >
                              {showKeys[provider.name] ? (
                                <EyeOff className="h-3.5 w-3.5" />
                              ) : (
                                <Eye className="h-3.5 w-3.5" />
                              )}
                            </button>
                          </div>
                        </div>
                      </div>
                    )}

                    <div className="space-y-1.5">
                      <Label htmlFor={`model-${provider.name}`} className="text-xs">
                        Model
                      </Label>
                      {provider.models?.length > 0 ? (
                        <Select
                          value={
                            selectedModels[provider.name] ||
                            provider.default_model
                          }
                          onValueChange={(val) =>
                            setSelectedModels((prev) => ({
                              ...prev,
                              [provider.name]: val,
                            }))
                          }
                        >
                          <SelectTrigger className="h-8 text-xs">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {provider.models.map((m) => (
                              <SelectItem key={m} value={m} className="text-xs">
                                {m}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      ) : (
                        <Input
                          id={`model-${provider.name}`}
                          value={
                            selectedModels[provider.name] ||
                            provider.default_model ||
                            ""
                          }
                          onChange={(e) =>
                            setSelectedModels((prev) => ({
                              ...prev,
                              [provider.name]: e.target.value,
                            }))
                          }
                          className="h-8 text-xs"
                        />
                      )}
                    </div>
                  </div>

                  {/* Save button */}
                  {(apiKeys[provider.name] ||
                    selectedModels[provider.name] ||
                    baseUrls[provider.name]) && (
                    <div className="flex justify-end">
                      <Button
                        size="sm"
                        onClick={() => handleSave(provider.name)}
                        disabled={isConfiguring}
                      >
                        {isConfiguring && (
                          <Loader2 className="mr-1 h-4 w-4 animate-spin" />
                        )}
                        Save
                      </Button>
                    </div>
                  )}

                  {/* Test result feedback */}
                  {testResult && !isTesting && (
                    <div
                      className={`rounded-md p-2 text-xs ${
                        testResult.healthy
                          ? "bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300"
                          : "bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300"
                      }`}
                    >
                      {testResult.healthy
                        ? `✓ Connected — Model: ${testResult.model}`
                        : `✗ Failed — ${testResult.error}`}
                    </div>
                  )}

                  {/* Balance warning bar (cloud providers only) */}
                  {balance && !isCheckingBalance && (
                    <>
                      {balance.status === "low_balance" && (
                        <div className="flex items-start gap-2 rounded-md bg-amber-50 p-3 dark:bg-amber-950 border border-amber-200 dark:border-amber-800">
                          <AlertTriangle className="h-4 w-4 text-amber-600 dark:text-amber-400 mt-0.5 shrink-0" />
                          <div className="text-xs text-amber-700 dark:text-amber-300">
                            <p className="font-medium">Low Balance</p>
                            <p>{balance.warning}</p>
                          </div>
                        </div>
                      )}
                      {balance.status === "rate_limited" && (
                        <div className="flex items-start gap-2 rounded-md bg-orange-50 p-3 dark:bg-orange-950 border border-orange-200 dark:border-orange-800">
                          <AlertTriangle className="h-4 w-4 text-orange-600 dark:text-orange-400 mt-0.5 shrink-0" />
                          <p className="text-xs text-orange-700 dark:text-orange-300">
                            {balance.warning ?? "Rate limited — your account is active but hitting usage limits."}
                          </p>
                        </div>
                      )}
                      {balance.status === "ok" && (
                        <div className="flex items-center gap-2 rounded-md bg-green-50 p-2 dark:bg-green-950 text-xs text-green-700 dark:text-green-300">
                          <CheckCircle className="h-3.5 w-3.5" />
                          {balance.message}
                        </div>
                      )}
                      {balance.status === "error" && (
                        <div className="flex items-center gap-2 rounded-md bg-red-50 p-2 dark:bg-red-950 text-xs text-red-700 dark:text-red-300">
                          <XCircle className="h-3.5 w-3.5" />
                          {balance.message}
                        </div>
                      )}
                    </>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
