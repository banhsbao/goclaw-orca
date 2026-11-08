import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import type { OrcaServerStatus } from "./hooks/use-orca-servers";

const VARIANTS = {
  healthy: "success",
  unhealthy: "warning",
  error: "destructive",
  cli_missing: "secondary",
  unknown: "outline",
} as const;

export function OrcaServerStatusBadge({ status }: { status: OrcaServerStatus }) {
  const { t } = useTranslation("orca-servers");
  return <Badge variant={VARIANTS[status] ?? "outline"}>{t(`status.${status}`, status)}</Badge>;
}
