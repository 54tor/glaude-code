import { Cog, Monitor, Moon, Sun } from "lucide-react"
import { FONTS, SIZES } from "@/lib/fonts"
import type { Choice } from "@/lib/models"
import { ModelPicker } from "./ModelPicker"
import { cn } from "@/lib/utils"
import { useTheme } from "@/components/theme-provider"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ScrollArea } from "@/components/ui/scroll-area"

const THEMES = [
  { v: "light", label: "Clair", Icon: Sun },
  { v: "dark", label: "Sombre", Icon: Moon },
  { v: "system", label: "Système", Icon: Monitor },
] as const

const KEYS: [string, string][] = [
  ["/", "rechercher"],
  ["j / k", "session suivante / précédente"],
  ["Entrée", "lire"],
  ["t", "reprendre dans un terminal"],
  ["m", "reprendre avec un message"],
  ["r", "renommer"],
  ["x", "sélectionner"],
  ["e", "exporter en Markdown"],
  ["Suppr", "supprimer"],
  ["Échap", "vider la sélection"],
]

// Réglages rarement modifiés : options passées à claude, thème, raccourcis.
export type Fonts = { font: string; setFont: (v: string) => void; fontSize: string; setFontSize: (v: string) => void }

// Police des conversations : un aperçu par jeu, en latin et en cyrillique, plus la taille.
function FontPicker({ font, setFont, fontSize, setFontSize }: Fonts) {
  return (
    <div className="space-y-1.5">
      <Label>Police des conversations</Label>
      <div className="grid gap-1 sm:grid-cols-2" role="radiogroup" aria-label="Police des conversations">
        {Object.entries(FONTS).map(([id, f]) => (
          <button
            key={id}
            role="radio"
            aria-checked={font === id}
            onClick={() => setFont(id)}
            className={cn(
              "block w-full rounded-md border px-2.5 py-1.5 text-left transition-colors",
              font === id ? "border-primary bg-accent" : "border-transparent hover:bg-muted"
            )}
          >
            <span className="block text-xs text-muted-foreground">{f.label}</span>
            <span className="block truncate text-sm" style={{ fontFamily: f.text }}>
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
              "rounded-md py-1 text-xs tabular-nums transition-colors",
              fontSize === v ? "bg-background font-medium shadow-sm" : "text-muted-foreground hover:text-foreground"
            )}
          >
            {v} px
          </button>
        ))}
      </div>
    </div>
  )
}

export function Settings({
  args,
  setArgs,
  choice,
  setChoice,
  ...fonts
}: { args: string; setArgs: (v: string) => void; choice: Choice; setChoice: (c: Choice) => void } & Fonts) {
  const { theme, setTheme } = useTheme()
  const section = (title: string, hint: string, body: React.ReactNode) => (
    <div className="space-y-1.5">
      <Label>{title}</Label>
      {body}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  )
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button variant="ghost" size="icon-sm" className="text-muted-foreground" aria-label="Réglages" title="Réglages">
          <Cog />
        </Button>
      </DialogTrigger>
      <DialogContent className="flex h-[min(85vh,560px)] flex-col gap-4 sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Réglages</DialogTitle>
          <DialogDescription>Enregistrés dans ce navigateur.</DialogDescription>
        </DialogHeader>
        <Tabs defaultValue="claude" className="min-h-0 flex-1">
          <TabsList>
            <TabsTrigger value="claude">Claude</TabsTrigger>
            <TabsTrigger value="look">Apparence</TabsTrigger>
            <TabsTrigger value="keys">Raccourcis</TabsTrigger>
          </TabsList>
          <ScrollArea className="mt-2 min-h-0 flex-1 pr-3">
            <TabsContent value="claude" className="space-y-5">
              {section(
                "Options ajoutées à claude",
                "Utilisées à chaque reprise dans un terminal. Le chat du lecteur n'en garde que --model, --fallback-model et --effort. Vide par défaut.",
                <Input
                  id="args"
                  value={args}
                  onChange={(e) => setArgs(e.target.value)}
                  placeholder="ex. --dangerously-skip-permissions"
                  className="font-mono text-xs"
                  spellCheck={false}
                  aria-label="Options ajoutées à claude"
                />
              )}
              {section(
                "Modèle et effort des nouveaux prompts",
                "Proposés par défaut pour reprendre avec un message, une nouvelle session ou le chat ; modifiables à chaque prompt.",
                <ModelPicker value={choice} onChange={setChoice} />
              )}
            </TabsContent>
            <TabsContent value="look" className="space-y-5">
              {section(
                "Thème",
                "",
                <div className="grid max-w-sm grid-cols-3 gap-1 rounded-lg bg-muted p-1">
                  {THEMES.map(({ v, label, Icon }) => (
                    <button
                      key={v}
                      onClick={() => setTheme(v)}
                      aria-pressed={theme === v}
                      className={cn(
                        "flex items-center justify-center gap-1.5 rounded-md py-1 text-xs transition-colors",
                        theme === v ? "bg-background font-medium shadow-sm" : "text-muted-foreground hover:text-foreground"
                      )}
                    >
                      <Icon className="size-3.5" /> {label}
                    </button>
                  ))}
                </div>
              )}
              <FontPicker {...fonts} />
            </TabsContent>
            <TabsContent value="keys">
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                {KEYS.map(([k, v]) => (
                  <div key={k} className="contents">
                    <dt>
                      <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono text-xs">{k}</kbd>
                    </dt>
                    <dd className="text-muted-foreground">{v}</dd>
                  </div>
                ))}
              </dl>
            </TabsContent>
          </ScrollArea>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
