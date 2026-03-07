// ─── LLM Settings ────────────────────────────────────────────────────────────
// Lists available LLM providers and allows testing connectivity.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { CheckCircle, XCircle, Loader2, Zap } from "lucide-react";
import { useLLMProviders } from "@/lib/api/queries";
import { useTestLLM } from "@/lib/api/mutations";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export function LLMSettings() {
  const { data: providers, isLoading } = useLLMProviders();
  const testLLM = useTestLLM();

  return (
    <Card className="max-w-2xl">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Zap className="h-5 w-5" />
          LLM Providers
        </CardTitle>
        <CardDescription>
          Available AI models for code review. Test connectivity to verify setup.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-16 w-full" />
            ))}
          </div>
        ) : !providers?.length ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            No LLM providers configured. Check server configuration.
          </p>
        ) : (
          <div className="space-y-3">
            {providers.map((provider) => (
              <div
                key={provider.name}
                className="flex items-center justify-between rounded-lg border p-4"
              >
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <p className="text-sm font-medium">{provider.name}</p>
                    {provider.healthy && (
                      <Badge variant="success">Healthy</Badge>
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {provider.model || provider.name}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    testLLM.mutate({ provider: provider.name })
                  }
                  disabled={testLLM.isPending}
                >
                  {testLLM.isPending &&
                  testLLM.variables?.provider === provider.name ? (
                    <Loader2 className="mr-1 h-4 w-4 animate-spin" />
                  ) : testLLM.data &&
                    testLLM.variables?.provider === provider.name ? (
                    testLLM.data.success ? (
                      <CheckCircle className="mr-1 h-4 w-4 text-green-500" />
                    ) : (
                      <XCircle className="mr-1 h-4 w-4 text-red-500" />
                    )
                  ) : null}
                  Test
                </Button>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
