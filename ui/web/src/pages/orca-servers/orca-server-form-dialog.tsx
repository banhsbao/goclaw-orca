import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { OrcaServer } from "./hooks/use-orca-servers";
import {
  validateOrcaServerForm,
  type OrcaServerFormState,
} from "./orca-server-form-helpers";

interface OrcaServerFormDialogProps {
  open: boolean;
  server: OrcaServer | null;
  onOpenChange: (open: boolean) => void;
  onSubmit: (form: OrcaServerFormState) => Promise<void>;
}

export function OrcaServerFormDialog({ open, server, onOpenChange, onSubmit }: OrcaServerFormDialogProps) {
  const { t } = useTranslation("orca-servers");
  const mode = server ? "edit" : "create";
  const [label, setLabel] = useState(server?.label ?? "");
  const [description, setDescription] = useState(server?.description ?? "");
  const [pairingCode, setPairingCode] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const form = { label, description, pairingCode };
    const invalid = validateOrcaServerForm(form, mode);
    if (invalid) {
      setError(t(`errors.${invalid}`));
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit(form);
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("toast.saveFailed"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!submitting) onOpenChange(v); }}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>
              {server ? t("form.editTitle", { label: server.label }) : t("form.createTitle")}
            </DialogTitle>
            <DialogDescription>
              {server ? t("form.editDesc") : t("form.createDesc")}
            </DialogDescription>
          </DialogHeader>

          <div className="mt-4 space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="orca-label">{t("form.label")}</Label>
              <Input
                id="orca-label"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                disabled={mode === "edit"}
                autoComplete="off"
                className="text-base md:text-sm"
              />
              {mode === "create" && <p className="text-xs text-muted-foreground">{t("form.labelHint")}</p>}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="orca-description">{t("form.description")}</Label>
              <Input
                id="orca-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                maxLength={200}
                className="text-base md:text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="orca-code">{t("form.pairingCode")}</Label>
              <Input
                id="orca-code"
                type="password"
                value={pairingCode}
                onChange={(e) => setPairingCode(e.target.value)}
                placeholder={t("form.pairingPlaceholder")}
                autoComplete="new-password"
                className="text-base md:text-sm"
              />
              {mode === "edit" && <p className="text-xs text-muted-foreground">{t("form.pairingKeep")}</p>}
            </div>
            {error && <p className="text-sm text-destructive">{error}</p>}
          </div>

          <DialogFooter className="mt-6">
            <Button type="button" variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>
              {t("form.cancel")}
            </Button>
            <Button type="submit" disabled={submitting}>
              {server ? t("form.save") : t("form.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
