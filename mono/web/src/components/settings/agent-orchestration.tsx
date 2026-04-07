// ─── Agent Orchestration Settings ────────────────────────────────────────────
// Per-role provider/model routing for the review agent swarm.
// Gated behind LLM configuration — users must configure at least one provider
// before they can access this tab.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useEffect, useMemo, useState } from "react";
import {
  CheckCircle,
  Loader2,
  RotateCcw,
  Save,
  Sparkles,
  AlertTriangle,
  Layers,
} from "lucide-react";
import { useLLMProviders, useLLMRoutes } from "@/lib/api/queries";
import { useSetLLMRoutes } from "@/lib/api/mutations";
import type { AgentRoute, LLMProvider } from "@/types/api";
import { AGENT_ROLES, AGENT_ROLE_META } from "@/types/api";
import type { AgentRoleId } from "@/types/api";
import { AgentAvatar } from "@/components/swarm/agent-avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { getLLMIcon } from "@/components/icons/brand-icons";

/** Sentinel value for "use primary / default" (no explicit route). */
const USE_DEFAULT = "__default__";

export function AgentOrchestration() {
  const { data: llmData, isLoading: loadingProviders } = useLLMProviders();
  const { data: routesData, isLoading: loadingRoutes } = useLLMRoutes();
  const setRoutes = useSetLLMRoutes();

  const providers = llmData?.providers ?? [];
  const primaryProvider = llmData?.primary ?? "";

  // Only show configured (healthy or at least API key set) providers.
  const configuredProviders = useMemo(
    () => providers.filter((p) => p.configured),
    [providers],
  );

  // Build a role → { provider, model } map from the server-returned routes.
  const serverRouteMap = useMemo(() => {
    const map: Record<string, { provider: string; model: string }> = {};
    for (const r of routesData?.routes ?? []) {
      map[r.role] = { provider: r.provider, model: r.model ?? "" };
    }
    return map;
  }, [routesData]);

  // Local edits — initialised from server state once loaded.
  const [localRoutes, setLocalRoutes] = useState<
    Record<string, { provider: string; model: string }>
  >({});
  const [routesEnabled, setRoutesEnabled] = useState(false);
  const [initialised, setInitialised] = useState(false);

  // Seed local state from server data on first load.
  useEffect(() => {
    if (!initialised && routesData) {
      setLocalRoutes(serverRouteMap);
      setRoutesEnabled(routesData.routes_enabled ?? false);
      setInitialised(true);
    }
  }, [routesData, serverRouteMap, initialised]);

  // Track if user has unsaved changes.
  const isDirty = useMemo(() => {
    if (!initialised) return false;
    // Check routes_enabled toggle change.
    if (routesEnabled !== (routesData?.routes_enabled ?? false)) return true;
    for (const role of AGENT_ROLES) {
      const local = localRoutes[role];
      const server = serverRouteMap[role];
      if (!local && !server) continue;
      if (!local || !server) return true;
      if (local.provider !== server.provider || local.model !== server.model) return true;
    }
    // Check if server has roles that local doesn't
    for (const role of Object.keys(serverRouteMap)) {
      if (!localRoutes[role]) return true;
    }
    return false;
  }, [localRoutes, serverRouteMap, initialised, routesEnabled, routesData]);

  // Saved-feedback flash.
  const [showSaved, setShowSaved] = useState(false);

  const handleProviderChange = (role: string, providerName: string) => {
    if (providerName === USE_DEFAULT) {
      // Remove the route — use primary.
      setLocalRoutes((prev) => {
        const next = { ...prev };
        delete next[role];
        return next;
      });
      return;
    }
    setLocalRoutes((prev) => ({
      ...prev,
      [role]: { provider: providerName, model: "" },
    }));
  };

  const handleModelChange = (role: string, model: string) => {
    setLocalRoutes((prev) => ({
      ...prev,
      [role]: { ...prev[role], model },
    }));
  };

  const handleSave = () => {
    const routes: AgentRoute[] = [];
    for (const [role, val] of Object.entries(localRoutes)) {
      if (val.provider) {
        routes.push({ role, provider: val.provider, model: val.model || undefined });
      }
    }
    setRoutes.mutate({ routes, routesEnabled }, {
      onSuccess: () => {
        setShowSaved(true);
        setTimeout(() => setShowSaved(false), 2500);
      },
    });
  };

  const handleReset = () => {
    setLocalRoutes(serverRouteMap);
    setRoutesEnabled(routesData?.routes_enabled ?? false);
  };

  const handleAutoAssign = () => {
    if (configuredProviders.length === 0) return;

    // Smart auto-assignment: complex roles → strongest provider, simple → fastest.
    // Ranking heuristic: anthropic > openai > gemini > grok > ollama.
    const ranking = ["anthropic", "openai", "gemini", "grok", "ollama"];
    const sorted = [...configuredProviders].sort((a, b) => {
      const ai = ranking.indexOf(a.name);
      const bi = ranking.indexOf(b.name);
      return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi);
    });

    const strongest = sorted[0];
    const fastest = sorted.length > 1 ? sorted[sorted.length - 1] : sorted[0];

    const complexRoles: AgentRoleId[] = ["orchestrator", "architect", "senior_dev", "security"];
    const simpleRoles: AgentRoleId[] = ["junior_dev", "qa", "docs", "ops", "ui_ux"];

    const auto: Record<string, { provider: string; model: string }> = {};
    for (const role of complexRoles) {
      auto[role] = { provider: strongest.name, model: "" };
    }
    for (const role of simpleRoles) {
      auto[role] = { provider: fastest.name, model: "" };
    }
    setLocalRoutes(auto);
  };

  // Models available for a given provider.
  const modelsFor = (providerName: string): string[] => {
    const p = providers.find((pr) => pr.name === providerName);
    return p?.models ?? [];
  };

  // Find display name.
  const displayName = (providerName: string): string => {
    const p = providers.find((pr) => pr.name === providerName);
    return p?.display_name || providerName;
  };

  const isLoading = loadingProviders || loadingRoutes;

  // ── Guard: no configured providers ────────────────────────────────────────
  if (!isLoading && configuredProviders.length === 0) {
    return (
      <Card className="max-w-3xl">
        <CardContent className="flex flex-col items-center justify-center py-12 text-center">
          <AlertTriangle className="h-10 w-10 text-amber-500 mb-4" />
          <h3 className="text-lg font-semibold mb-2">Complete LLM Configuration First</h3>
          <p className="text-sm text-muted-foreground max-w-md">
            Agent Orchestration requires at least one configured LLM provider.
            Go to the <span className="font-medium">LLM Configuration</span> tab to add
            your API key and test the connection, then come back here to configure
            per-agent model routing.
          </p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="max-w-3xl">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <Sparkles className="h-5 w-5" />
              Agent Orchestration
            </CardTitle>
            <CardDescription className="mt-1.5">
              Assign LLM providers and models to each agent role in the review swarm.
              Unassigned roles use the primary provider
              {primaryProvider && (
                <>
                  {" "}(<span className="font-medium">{displayName(primaryProvider)}</span>)
                </>
              )}.
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            {configuredProviders.length > 1 && (
              <Button
                variant="outline"
                size="sm"
                onClick={handleAutoAssign}
                disabled={!routesEnabled}
                title={routesEnabled ? "Auto-assign providers to roles" : "Enable model routing first"}
              >
                <Sparkles className="mr-1 h-3.5 w-3.5" />
                Auto-Assign
              </Button>
            )}
          </div>
        </div>

        {/* Multi-LLM toggle */}
        <div className="mt-4 flex items-start gap-3 rounded-lg border border-dashed p-3 bg-muted/30">
          <div className="pt-0.5">
            <Switch
              id="routes-enabled"
              checked={routesEnabled}
              onCheckedChange={setRoutesEnabled}
            />
          </div>
          <div className="flex-1 min-w-0">
            <label
              htmlFor="routes-enabled"
              className="text-sm font-medium cursor-pointer flex items-center gap-1.5"
            >
              <Layers className="h-4 w-4 text-muted-foreground" />
              Enable agent-specific model routing
            </label>
            <p className="text-xs text-muted-foreground mt-0.5">
              {routesEnabled
                ? "Each agent is pinned to its assigned model below. Agents talk to one LLM per turn."
                : "Agents use multi-LLM probing — querying multiple providers in parallel and using consensus to select the best response. Recommended for higher-quality results."}
            </p>
          </div>
        </div>
      </CardHeader>

      <CardContent>
        {isLoading ? (
          <div className="space-y-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full" />
            ))}
          </div>
        ) : (
          <>
            {/* Provider summary badges */}
            <div className="flex flex-wrap gap-2 mb-6">
              {configuredProviders.map((p) => (
                <Badge
                  key={p.name}
                  variant={p.healthy ? "success" : "secondary"}
                  className="text-xs"
                >
                  {p.display_name || p.name}
                  {p.name === primaryProvider && " ★"}
                  {p.models?.length > 0 && ` (${p.models.length} models)`}
                </Badge>
              ))}
            </div>

            <Separator className="mb-6" />

            {/* Role assignment grid — dimmed when routes are disabled */}
            <div className={`space-y-3 transition-opacity ${routesEnabled ? "" : "opacity-40 pointer-events-none"}`}>
              {!routesEnabled && (
                <p className="text-xs text-muted-foreground italic mb-2 pointer-events-auto opacity-100">
                  Agent model routing is disabled — agents will query multiple LLMs and use consensus.
                  Enable the toggle above to pin agents to specific models.
                </p>
              )}
              {AGENT_ROLES.map((role) => {
                const meta = AGENT_ROLE_META[role];
                const route = localRoutes[role];
                const selectedProvider = route?.provider ?? USE_DEFAULT;
                const selectedModel = route?.model ?? "";
                const available = selectedProvider !== USE_DEFAULT
                  ? modelsFor(selectedProvider)
                  : [];

                return (
                  <div
                    key={role}
                    className="grid grid-cols-[1fr_1fr_1fr] gap-3 items-center rounded-lg border p-3"
                  >
                    {/* Role info */}
                    <div className="flex items-center gap-2 min-w-0">
                      <AgentAvatar role={role} size="sm" />
                      <div className="min-w-0">
                        <p className="text-sm font-medium truncate">{meta.label}</p>
                        <p className="text-xs text-muted-foreground truncate">
                          {meta.description}
                        </p>
                      </div>
                    </div>

                    {/* Provider selector */}
                    <Select
                      value={selectedProvider}
                      onValueChange={(val) => handleProviderChange(role, val)}
                    >
                      <SelectTrigger className="h-8 text-xs">
                        <SelectValue placeholder="Primary (default)" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value={USE_DEFAULT} className="text-xs">
                          Primary ({displayName(primaryProvider)})
                        </SelectItem>
                        {configuredProviders.map((p) => {
                          const PIcon = getLLMIcon(p.name);
                          return (
                            <SelectItem key={p.name} value={p.name} className="text-xs">
                              <span className="flex items-center gap-1.5">
                                {PIcon && <PIcon size={14} />}
                                {p.display_name || p.name}
                              </span>
                            </SelectItem>
                          );
                        })}
                      </SelectContent>
                    </Select>

                    {/* Model selector */}
                    <Select
                      value={selectedModel || "__provider_default__"}
                      onValueChange={(val) =>
                        handleModelChange(role, val === "__provider_default__" ? "" : val)
                      }
                      disabled={selectedProvider === USE_DEFAULT}
                    >
                      <SelectTrigger className="h-8 text-xs">
                        <SelectValue placeholder="Provider default" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__provider_default__" className="text-xs">
                          Provider default
                        </SelectItem>
                        {available.map((m) => (
                          <SelectItem key={m} value={m} className="text-xs">
                            {m}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                );
              })}
            </div>

            {/* Column headers — subtle labels above the grid */}
            {/* Action bar */}
            <div className="flex items-center justify-between mt-6 pt-4 border-t">
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                {isDirty && (
                  <span className="text-amber-600 dark:text-amber-400 flex items-center gap-1">
                    <AlertTriangle className="h-3 w-3" />
                    Unsaved changes
                  </span>
                )}
                {showSaved && (
                  <span className="text-green-600 dark:text-green-400 flex items-center gap-1">
                    <CheckCircle className="h-3 w-3" />
                    Routes saved
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={handleReset}
                  disabled={!isDirty || setRoutes.isPending}
                >
                  <RotateCcw className="mr-1 h-3.5 w-3.5" />
                  Reset
                </Button>
                <Button
                  size="sm"
                  onClick={handleSave}
                  disabled={!isDirty || setRoutes.isPending}
                >
                  {setRoutes.isPending ? (
                    <Loader2 className="mr-1 h-4 w-4 animate-spin" />
                  ) : (
                    <Save className="mr-1 h-3.5 w-3.5" />
                  )}
                  Save Routes
                </Button>
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
