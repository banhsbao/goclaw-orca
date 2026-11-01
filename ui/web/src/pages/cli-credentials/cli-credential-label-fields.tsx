import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface Props {
  label: string;
  onLabelChange: (v: string) => void;
  description: string;
  onDescriptionChange: (v: string) => void;
  labelLocked: boolean;
}

export function CliCredentialLabelFields({ label, onLabelChange, description, onDescriptionChange, labelLocked }: Props) {
  const { t } = useTranslation("cli-credentials");
  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-1.5">
        <Label>{t("credentialLabels.label")}</Label>
        <Input
          value={label}
          onChange={(e) => onLabelChange(e.target.value.toLowerCase())}
          placeholder={t("credentialLabels.labelPlaceholder")}
          maxLength={32}
          disabled={labelLocked}
          className="font-mono text-base md:text-sm"
        />
        <p className="text-xs text-muted-foreground">{t("credentialLabels.labelHint")}</p>
      </div>
      <div className="grid gap-1.5">
        <Label>{t("credentialLabels.description")}</Label>
        <Input
          value={description}
          onChange={(e) => onDescriptionChange(e.target.value)}
          placeholder={t("credentialLabels.descriptionPlaceholder")}
          maxLength={200}
          className="text-base md:text-sm"
        />
      </div>
    </div>
  );
}
