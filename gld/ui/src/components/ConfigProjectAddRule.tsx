import { useState } from "react"
import { Plus } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Choose } from "./Choose"

const RULE_KINDS = ["allow", "ask", "deny"] as const
const SHARED_LAYER = "p"
const PERSONAL_LAYER = "l"

type Props = { projectDir: string; onAddRule: (file: string, kind: string, rule: string) => void }

export function ProjectAddRule({ projectDir, onAddRule }: Props) {
  const { t } = useTranslation()
  const [ruleText, setRuleText] = useState("")
  const [ruleKind, setRuleKind] = useState<string>("allow")
  const [layer, setLayer] = useState(PERSONAL_LAYER)
  const trimmedRule = ruleText.trim()

  const submitRule = () => {
    if (!trimmedRule) return
    onAddRule(`${layer}/${projectDir}`, ruleKind, trimmedRule)
    setRuleText("")
  }

  return (
    <form
      className="mb-2 flex flex-wrap items-center gap-2"
      onSubmit={(submitEvent) => {
        submitEvent.preventDefault()
        submitRule()
      }}
    >
      <Input value={ruleText} onChange={(changeEvent) => setRuleText(changeEvent.target.value)} placeholder={t("config.projects.addRule.placeholder")} aria-label={t("config.projects.addRule.aria")} className="h-8 w-64" />
      <Choose value={ruleKind} onChange={setRuleKind} options={RULE_KINDS.map((kind) => [kind, t(`config.perms.kind.${kind}`)] as const)} label={t("config.projects.addRule.kindAria")} size="sm" />
      <Choose
        value={layer}
        onChange={setLayer}
        options={[
          [PERSONAL_LAYER, t("config.projects.addRule.layerPersonal")],
          [SHARED_LAYER, t("config.projects.addRule.layerShared")],
        ]}
        label={t("config.projects.addRule.layerAria")}
        size="sm"
      />
      <Button type="submit" size="xs" variant="secondary" disabled={!trimmedRule}>
        <Plus /> {t("config.projects.addRule.submit")}
      </Button>
    </form>
  )
}
