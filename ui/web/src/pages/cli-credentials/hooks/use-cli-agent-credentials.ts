import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";
import type { CLIAgentCredential, CLIAgentCredentialInput } from "@/types/cli-credential";

export type { CLIAgentCredential, CLIAgentCredentialInput };

function labelQuery(label: string) {
  return label ? `?label=${encodeURIComponent(label)}` : "";
}

/** Hook for managing agent-scoped credentials on a specific CLI binary. */
export function useCliAgentCredentials(binaryId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();
  const queryKey = ["cliCredentials", binaryId, "agentCredentials"] as const;

  const { data, isLoading } = useQuery({
    queryKey,
    queryFn: async () => {
      const res = await http.get<{ agent_credentials: CLIAgentCredential[] }>(
        `/v1/cli-credentials/${binaryId}/agent-credentials`,
      );
      return res.agent_credentials ?? [];
    },
    enabled: !!binaryId,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey }),
    [queryClient, queryKey],
  );

  const getCredential = useCallback(
    (agentId: string, label = "") => http.get<CLIAgentCredential>(
      `/v1/cli-credentials/${binaryId}/agent-credentials/${agentId}${labelQuery(label)}`,
    ),
    [http, binaryId],
  );

  const setCredential = useCallback(
    async (agentId: string, input: CLIAgentCredentialInput, label = "") => {
      try {
        await http.put(`/v1/cli-credentials/${binaryId}/agent-credentials/${agentId}${labelQuery(label)}`, input);
        await invalidate();
        toast.success(i18n.t("cli-credentials:agentCredentials.saved"));
      } catch (err) {
        toast.error(
          i18n.t("cli-credentials:agentCredentials.saveFailed"),
          err instanceof Error ? err.message : "",
        );
        throw err;
      }
    },
    [http, binaryId, invalidate],
  );

  const deleteCredential = useCallback(
    async (agentId: string, label = "") => {
      try {
        await http.delete(`/v1/cli-credentials/${binaryId}/agent-credentials/${agentId}${labelQuery(label)}`);
        await invalidate();
        toast.success(i18n.t("cli-credentials:agentCredentials.deleted"));
      } catch (err) {
        toast.error(
          i18n.t("cli-credentials:agentCredentials.deleteFailed"),
          err instanceof Error ? err.message : "",
        );
        throw err;
      }
    },
    [http, binaryId, invalidate],
  );

  return {
    agentCredentials: data ?? [],
    loading: isLoading,
    getCredential,
    setCredential,
    deleteCredential,
  };
}
