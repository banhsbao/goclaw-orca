import type { OrcaServerCreateInput, OrcaServerUpdateInput } from "./hooks/use-orca-servers";

export interface OrcaServerFormState {
  label: string;
  description: string;
  pairingCode: string;
}

export type OrcaServerFormError =
  | "labelInvalid"
  | "labelReserved"
  | "descriptionTooLong"
  | "codeRequired"
  | "codeInvalid";

const LABEL_RE = /^[a-z0-9][a-z0-9_-]{0,31}$/;
const MAX_DESCRIPTION = 200;
const CODE_PREFIX = "orca://";

export function validateOrcaServerForm(
  form: OrcaServerFormState,
  mode: "create" | "edit",
): OrcaServerFormError | null {
  if (mode === "create") {
    if (!LABEL_RE.test(form.label)) return "labelInvalid";
    if (form.label === "default") return "labelReserved";
  }
  if (form.description.length > MAX_DESCRIPTION) return "descriptionTooLong";
  const code = form.pairingCode.trim();
  if (mode === "create" && !code) return "codeRequired";
  if (code && !code.startsWith(CODE_PREFIX)) return "codeInvalid";
  return null;
}

export function buildOrcaServerCreatePayload(form: OrcaServerFormState): OrcaServerCreateInput {
  const description = form.description.trim();
  return {
    label: form.label,
    ...(description ? { description } : {}),
    pairing_code: form.pairingCode.trim(),
  };
}

export function buildOrcaServerUpdatePayload(form: OrcaServerFormState): OrcaServerUpdateInput {
  const code = form.pairingCode.trim();
  return {
    description: form.description.trim(),
    ...(code ? { pairing_code: code } : {}),
  };
}
