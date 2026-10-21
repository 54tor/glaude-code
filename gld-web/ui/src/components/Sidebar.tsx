import {
  AlarmClock,
  Star,
  Brain,
  Flame,
  History,
  Clock,
  FileText,
  Inbox,
  Loader2,
  Monitor,
  Moon,
  Search,
  Settings2,
  ShieldCheck,
  Sun,
  Tag,
  X,
} from "lucide-react"
import { size } from "@/lib/format"
import { FONTS, SIZES } from "@/lib/fonts"
import type { Choice } from "@/lib/models"
import { ModelPicker } from "./ModelPicker"
import type { Route } from "@/lib/route"
import type { Node } from "@/lib/tree"
import { cn } from "@/lib/utils"
import { useTheme } from "@/components/theme-provider"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { ProjectTree } from "./ProjectTree"
import { CM, GroupItems, SessionItems } from "./SessionMenu"
import type { Session, VGroup } from "@/lib/api"
import { useActions, type Row } from "./actions"

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
type Fonts = { font: string; setFont: (v: string) => void; fontSize: string; setFontSize: (v: string) => void }

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

function Settings({
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
        <Button variant="ghost" size="sm" className="text-muted-foreground">
          <Settings2 /> Réglages
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

function NavItem({ icon: Icon, label, n, active, onClick }: { icon: typeof Inbox; label: string; n?: number; active: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      aria-current={active}
      className={cn(
        "flex h-8 w-full items-center gap-2 rounded-md px-2 text-sm transition-colors",
        active ? "bg-accent font-medium" : "text-foreground/80 hover:bg-muted hover:text-foreground"
      )}
    >
      <Icon className="size-4 shrink-0 text-muted-foreground" />
      <span className="flex-1 truncate text-left">{label}</span>
      {n !== undefined && <span className="text-xs text-muted-foreground tabular-nums">{n}</span>}
    </button>
  )
}

function Heading({ children, action }: { children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="mt-4 mb-1 flex items-center px-2">
      <p className="flex-1 text-xs font-medium text-muted-foreground">{children}</p>
      {action}
    </div>
  )
}

// Sessions actives depuis moins d'une heure : leur cache est chaud, les reprendre coûte peu.
// En orange, celles dont le cache expire dans moins de 10 minutes.
function Hot({ rows, now, ttl }: { rows: Row[]; now: number; ttl: number }) {
  const a = useActions()
  return (
    <>
      <Heading>
        <span title="Le cache des prompts dure une heure : reprendre une de ces sessions maintenant est plus rapide et moins coûteux">
          Cache chaud
        </span>
      </Heading>
      {rows.slice(0, 5).map(({ s }) => {
        const left = Math.max(1, Math.ceil((ttl - (now - s.mtime)) / 60))
        const warn = left <= 10
        return (
          <button
            key={s.id}
            onClick={() => a.read(s)}
            title={`${s.title}\nCache encore environ ${left} min`}
            className="flex h-8 w-full items-center gap-2 rounded-md px-2 text-sm text-foreground/80 transition-colors hover:bg-muted hover:text-foreground"
          >
            {warn ? (
              <AlarmClock className="size-4 shrink-0 text-amber-500" aria-label="cache bientôt expiré" />
            ) : (
              <Flame className="size-4 shrink-0 text-orange-500" aria-hidden />
            )}
            {a.live.has(s.id) && <span className="size-1.5 shrink-0 rounded-full bg-emerald-500" aria-label="ouverte" />}
            <span className="flex-1 truncate text-left">{s.title}</span>
            <span className={cn("shrink-0 text-xs tabular-nums", warn ? "font-medium text-amber-600 dark:text-amber-400" : "text-muted-foreground")}>
              {left} min
            </span>
          </button>
        )
      })}
    </>
  )
}

export type SidebarProps = {
  loading: boolean
  total: number
  q: string
  setQ: (v: string) => void
  searchRef: React.RefObject<HTMLInputElement | null>
  searching: boolean
  route: Route
  go: (r: Route) => void
  counts: { recent: number; all: number }
  favRows: Row[] // sessions favorites
  favGroups: { vg: VGroup; key: string }[] // groupes favoris, avec la clé de leur dossier
  groupCount: (vg: VGroup) => number
  read: (s: Session) => void
  tree: Node[]
  hot: Row[] // sessions dont le cache des prompts est encore chaud
  now: number
  ttl: number // durée du cache, en secondes
  snippets: number | null // null : bibliothèque non configurée
  auto: { n: number; kb: number; purge: () => void }
  args: string
  setArgs: (v: string) => void
  choice: Choice
  setChoice: (c: Choice) => void
} & Fonts

export function Sidebar({ searchRef, ...p }: SidebarProps) {
  const r = p.route
  return (
    <aside className="flex max-h-[45dvh] shrink-0 flex-col border-b bg-sidebar md:max-h-none md:w-72 md:border-r md:border-b-0 lg:w-80">
      <div className="space-y-3 p-3">
        <div className="flex items-center gap-2 px-1 pt-1">
          <img src="/icons/icon-192.png" alt="" className="size-8 shrink-0" />
          <div className="leading-tight">
            <h1 className="text-sm font-semibold">Glaude Code</h1>
            <p className="text-xs text-muted-foreground">{p.loading ? "Chargement…" : `${p.total} sessions`}</p>
          </div>
        </div>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            ref={searchRef}
            value={p.q}
            onChange={(e) => p.setQ(e.target.value)}
            onKeyDown={(e) => e.key === "Escape" && p.setQ("")}
            placeholder="Rechercher…"
            className="bg-background pr-8 pl-8"
            aria-label="Rechercher dans les titres, les projets et les conversations"
            autoFocus
          />
          {p.searching ? (
            <Loader2
              className="absolute top-1/2 right-2.5 size-4 -translate-y-1/2 animate-spin text-muted-foreground"
              aria-label="Recherche en cours"
            />
          ) : p.q ? (
            <button
              className="absolute top-1/2 right-2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              onClick={() => p.setQ("")}
              aria-label="Effacer la recherche"
            >
              <X className="size-4" />
            </button>
          ) : (
            <kbd className="pointer-events-none absolute top-1/2 right-2 -translate-y-1/2 rounded border bg-muted px-1.5 font-mono text-[10px] text-muted-foreground">
              /
            </kbd>
          )}
        </div>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <nav className="px-2 pb-3" aria-label="Navigation">
          <NavItem
            icon={Clock}
            label="Récentes (7 jours)"
            n={p.counts.recent}
            active={r.kind === "recent"}
            onClick={() => p.go({ kind: "recent" })}
          />
          <NavItem icon={Inbox} label="Toutes les sessions" n={p.counts.all} active={r.kind === "all"} onClick={() => p.go({ kind: "all" })} />
          <NavItem icon={History} label="Sessions sauvegardées" active={r.kind === "tabs"} onClick={() => p.go({ kind: "tabs" })} />
          <NavItem icon={Brain} label="Mémoire" active={r.kind === "memory"} onClick={() => p.go({ kind: "memory", key: "" })} />
          {p.snippets !== null && (
            <NavItem
              icon={FileText}
              label="Snippets"
              n={p.snippets}
              active={r.kind === "snippets"}
              onClick={() => p.go({ kind: "snippets", path: "" })}
            />
          )}

          {p.hot.length > 0 && <Hot rows={p.hot} now={p.now} ttl={p.ttl} />}

          {(p.favRows.length > 0 || p.favGroups.length > 0) && (
            <>
              <Heading>Favoris</Heading>
              {p.favGroups.map(({ vg, key }) => (
                <ContextMenu key={key + "\0" + vg.name}>
                  <ContextMenuTrigger asChild>
                    <div>
                      <NavItem
                        icon={Tag}
                        label={vg.name}
                        n={p.groupCount(vg)}
                        active={r.kind === "group" && r.name === vg.name && r.key === key}
                        onClick={() => p.go({ kind: "group", name: vg.name, key })}
                      />
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent className="w-72">
                    <GroupItems M={CM} vg={vg} />
                  </ContextMenuContent>
                </ContextMenu>
              ))}
              {p.favRows.map((row) => (
                <ContextMenu key={row.s.id}>
                  <ContextMenuTrigger asChild>
                    <div>
                      <NavItem icon={Star} label={row.s.title} active={false} onClick={() => p.read(row.s)} />
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent className="w-64">
                    <SessionItems M={CM} row={row} full />
                  </ContextMenuContent>
                </ContextMenu>
              ))}
            </>
          )}

          {(p.loading || p.tree.length > 0) && <Heading>Projets</Heading>}
          {p.loading ? (
            <div className="space-y-2 px-2">
              <Skeleton className="h-6 w-full" />
              <Skeleton className="h-6 w-4/5" />
              <Skeleton className="h-6 w-3/5" />
            </div>
          ) : (
            <ProjectTree
              nodes={p.tree}
              sel={r.kind === "dir" ? r.key : ""}
              onSel={(key) => p.go({ kind: "dir", key })}
              selGroup={r.kind === "group" ? r.key + "\0" + r.name : ""}
              onGroup={(vg, key) => p.go({ kind: "group", name: vg.name, key })}
            />
          )}
        </nav>
      </ScrollArea>

      <div className="space-y-2 border-t p-3">
        {p.auto.n > 0 && (
          <div className="flex items-center gap-2 rounded-lg border bg-background px-3 py-2 text-xs">
            <ShieldCheck className="size-4 shrink-0 text-muted-foreground" />
            <span className="flex-1 text-muted-foreground">
              {p.auto.n} revues de sécurité masquées · {size(p.auto.kb)}
            </span>
            <Button variant="ghost" size="xs" className="text-destructive hover:text-destructive" onClick={p.auto.purge}>
              Nettoyer
            </Button>
          </div>
        )}
        <div className="flex items-center gap-2">
          <Settings
            args={p.args}
            setArgs={p.setArgs}
            choice={p.choice}
            setChoice={p.setChoice}
            font={p.font}
            setFont={p.setFont}
            fontSize={p.fontSize}
            setFontSize={p.setFontSize}
          />
          {p.args && (
            <code className="truncate text-xs text-muted-foreground" title="options ajoutées à claude">
              {p.args}
            </code>
          )}
        </div>
      </div>
    </aside>
  )
}
