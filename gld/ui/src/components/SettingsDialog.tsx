import { Monitor, Moon, Sun } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { setLang, type Lang } from "@/i18n"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { FONTS, SIZES } from "@/lib/fonts"
import type { Choice } from "@/lib/models"
import { Choose } from "./Choose"
import { ModelPicker } from "./ModelPicker"
import { cn } from "@/lib/utils"
import { useTheme } from "@/components/theme-provider"
import { PALETTES } from "@/lib/palettes"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

const LANGS: { v: Lang; label: string; claude: string }[] = [
  { v: "fr", label: "Français", claude: "French" },
  { v: "en", label: "English", claude: "English" },
]

const THEMES = [
  { v: "light", label: "themeLight", Icon: Sun },
  { v: "dark", label: "themeDark", Icon: Moon },
  { v: "system", label: "themeSystem", Icon: Monitor },
] as const

// [touche affichée (cap.* : nom traduit), id du libellé]
const KEYS: [string, string][] = [
  ["/", "search"],
  ["j / k", "nav"],
  ["cap.enter", "enter"],
  ["t", "t"],
  ["m", "m"],
  ["r", "r"],
  ["x", "x"],
  ["e", "e"],
  ["cap.del", "del"],
  ["cap.esc", "esc"],
]

// Réglages rarement modifiés : options passées à claude, thème, raccourcis.
export type Fonts = {
  font: string
  setFont: (v: string) => void
  fontSize: string
  setFontSize: (v: string) => void
}

// Police des conversations : un aperçu par jeu, en latin et en cyrillique, plus la taille.
function FontPicker({ font, setFont, fontSize, setFontSize }: Fonts) {
  const { t } = useTranslation()
  return (
    <div className="space-y-1.5">
      <Label>{t("config.dialog.font")}</Label>
      <div
        className="grid gap-1 sm:grid-cols-2"
        role="radiogroup"
        aria-label={t("config.dialog.font")}
      >
        {Object.entries(FONTS).map(([id, f]) => (
          <button
            key={id}
            role="radio"
            aria-checked={font === id}
            onClick={() => setFont(id)}
            className={cn(
              "block w-full rounded-md border px-2.5 py-1.5 text-left transition-colors",
              font === id
                ? "border-primary bg-accent text-accent-foreground"
                : "border-transparent hover:bg-muted"
            )}
          >
            <span className="block text-xs text-muted-foreground">
              {f.label}
            </span>
            <span
              className="block truncate text-sm"
              style={{ fontFamily: f.text }}
            >
              Ça marche — Привет, мир ! 0O 1lI
            </span>
          </button>
        ))}
      </div>
      <div className="grid max-w-sm grid-cols-5 gap-1 rounded-lg bg-muted p-1">
        {SIZES.map((v) => (
          <button
            key={v}
            onClick={() => setFontSize(v)}
            aria-pressed={fontSize === v}
            className={cn(
              "rounded-md py-1 text-xs tabular-nums transition-colors pointer-coarse:min-h-8",
              fontSize === v
                ? "bg-background font-medium shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            {v} px
          </button>
        ))}
      </div>
    </div>
  )
}

export type UiSettings = {
  args: string
  setArgs: (v: string) => void
  choice: Choice
  setChoice: (c: Choice) => void
} & Fonts

const section = (title: string, hint: string, body: React.ReactNode) => (
  <div className="space-y-1.5">
    <Label>{title}</Label>
    {body}
    {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
  </div>
)

// Options de l'interface passées à claude (arguments, modèle et effort par défaut) : en tête de l'onglet Réglages.
export function ClaudeOptions({ args, setArgs, choice, setChoice }: Pick<UiSettings, "args" | "setArgs" | "choice" | "setChoice">) {
  const { t } = useTranslation()
  return (
    <section className="space-y-5">
      <h3 className="border-b pb-1 text-md font-semibold">
        {t("config.dialog.tabClaude")}
      </h3>
      {section(
        t("config.dialog.argsTitle"),
        t("config.dialog.argsHint"),
        <Input
          id="args"
          value={args}
          onChange={(e) => setArgs(e.target.value)}
          placeholder={t("config.dialog.argsPlaceholder")}
          className="font-mono text-xs"
          spellCheck={false}
          aria-label={t("config.dialog.argsTitle")}
        />
      )}
      {section(
        t("config.dialog.modelTitle"),
        t("config.dialog.modelHint"),
        <ModelPicker value={choice} onChange={setChoice} />
      )}
    </section>
  )
}

// Réglages de l'interface (raccourcis, apparence), dans la page Configuration.
export function SettingsPanel(fonts: Fonts) {
  const { t, i18n } = useTranslation()
  const { theme, setTheme, palette, setPalette } = useTheme()
  // La langue est le réglage « language » de Claude Code (settings.json) : l'interface change tout de suite, le serveur et les commandes la lisent au prochain appel.
  const pickLang = async (lang: Lang, claude: string) => {
    setLang(lang)
    try {
      await api.configEdit({ file: "u", op: "set", path: ["language"], value: claude })
    } catch (e) {
      toast.error(errMsg(e))
    }
  }
  return (
    <div className="space-y-8">
      <section className="space-y-3">
        <h3 className="border-b pb-1 text-md font-semibold">
          {t("config.dialog.tabKeys")}
        </h3>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          {KEYS.map(([k, v]) => (
            <div key={k} className="contents">
              <dt>
                <kbd className="rounded-sm border bg-muted px-1.5 py-0.5 font-mono text-xs">
                  {k.startsWith("cap.") ? t(`config.dialog.${k}`) : k}
                </kbd>
              </dt>
              <dd className="text-muted-foreground">
                {t(`config.dialog.key.${v}`)}
              </dd>
            </div>
          ))}
        </dl>
      </section>
      <section className="space-y-5">
        <h3 className="border-b pb-1 text-md font-semibold">
          {t("config.dialog.tabLook")}
        </h3>
        {section(
          t("config.dialog.language"),
          t("config.dialog.languageHint"),
          <Choose
            value={i18n.language}
            onChange={(v) =>
              pickLang(v as Lang, LANGS.find((l) => l.v === v)!.claude)
            }
            options={LANGS.map((l) => [l.v, l.label] as const)}
            label={t("config.dialog.language")}
            size="default"
            className="w-full max-w-sm"
          />
        )}
        {section(
          t("config.dialog.theme"),
          "",
          <div className="grid max-w-sm grid-cols-3 gap-1 rounded-lg bg-muted p-1">
            {THEMES.map(({ v, label, Icon }) => (
              <button
                key={v}
                onClick={() => setTheme(v)}
                aria-pressed={theme === v}
                className={cn(
                  "flex items-center justify-center gap-1.5 rounded-md py-1 text-xs transition-colors pointer-coarse:min-h-8",
                  theme === v
                    ? "bg-background font-medium shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                )}
              >
                <Icon className="size-3.5" /> {t(`config.dialog.${label}`)}
              </button>
            ))}
          </div>
        )}
        {section(
          t("config.dialog.palette"),
          "",
          <div
            className="grid max-w-md grid-cols-2 gap-1.5"
            role="radiogroup"
            aria-label={t("config.dialog.palette")}
          >
            {Object.entries(PALETTES).map(([id, p]) => {
              const t = p.dark
              return (
                <button
                  key={id}
                  role="radio"
                  aria-checked={palette === id}
                  onClick={() => setPalette(id)}
                  className={cn(
                    "flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-left text-xs transition-colors pointer-coarse:min-h-8",
                    palette === id
                      ? "border-primary bg-accent font-medium"
                      : "hover:bg-muted"
                  )}
                >
                  <span className="flex shrink-0 -space-x-1" aria-hidden>
                    {(t
                      ? [t.bg, t.primary, t.fg]
                      : ["#1e2530", "#7fd6e6", "#e6e9ee"]
                    ).map((c) => (
                      <span
                        key={c}
                        className="size-3.5 rounded-full border border-black/20"
                        style={{ background: c }}
                      />
                    ))}
                  </span>
                  {p.name}
                </button>
              )
            })}
          </div>
        )}
        <FontPicker {...fonts} />
      </section>
    </div>
  )
}
