import { Coins, Flame, Snowflake } from "lucide-react"
import { toast } from "sonner"
import { api, type Session, type Tokens } from "@/lib/api"
import { compact, tokTotal } from "@/lib/format"
import { cn } from "@/lib/utils"
import { refreshWarm, useKeepWarm } from "@/hooks/useKeepWarm"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useActions } from "./actions"
import type { Live } from "./LiveStatus"

const set = async (s: Session, on: boolean, max?: number) => {
  const r = await api.post("/keepwarm", { dir: s.dir, id: s.id, on, max })
  if (!r.ok) toast.error(r.out)
  refreshWarm()
}

// Bouton de la barre d'outils : active ou arrête le maintien. L'activation se fait par un menu qui dit ce que ça coûte.
export function KeepWarmButton({ s }: { s: Session }) {
  const a = useActions()
  const warm = useKeepWarm()[s.id]
  if (!a.caps(s).keepWarm) return null // fonction propre à certains fournisseurs
  if (warm?.on)
    return (
      <Button variant="outline" size="sm" className="border-amber-500/60 text-amber-600 dark:text-amber-400" onClick={() => set(s, false)} title="Arrêter le maintien au chaud">
        <Coins /> Maintien actif : arrêter
      </Button>
    )
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" title="Garde le cache des prompts chaud en envoyant un ping automatique (payant)">
          <Flame /> Maintenir au chaud
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-80">
        <DropdownMenuLabel className="whitespace-normal">Dès qu'il reste moins de 5 min de cache, gld envoie un court message à la session. Chaque ping est un tour facturé et apparaît dans la conversation.</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {[3, 6, 12, 24].map((n) => (
          <DropdownMenuItem key={n} onSelect={() => set(s, true, n)}>
            Au plus {n} ping{n > 1 ? "s" : ""} (≈ {n} h)
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// Bandeau toujours visible tant que le maintien est actif : c'est de l'argent dépensé sans action de l'utilisateur.
export function KeepWarmBanner({ s }: { s: Session }) {
  const a = useActions()
  const warm = useKeepWarm()[s.id]
  if (!a.caps(s).keepWarm || (!warm?.on && !warm?.note)) return null
  return (
    <div className={cn("flex flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-1.5 text-xs md:px-6", warm.on ? "border-amber-500/40 bg-amber-500/15 text-amber-700 dark:text-amber-300" : "bg-muted/40 text-muted-foreground")}>
      <Coins className="size-4 shrink-0" />
      {warm.on ? (
        <>
          <span className="font-medium">Maintien au chaud actif — ça coûte de l'argent.</span>
          <span className="tabular-nums">
            {warm.pings}/{warm.max} ping{warm.pings > 1 ? "s" : ""}
            {warm.cost > 0 && ` · ${warm.cost.toFixed(3)} $ déjà dépensés`}
          </span>
          <span>un ping part quand il reste moins de 5 min de cache</span>
        </>
      ) : (
        <span>{warm.note}</span>
      )}
      {warm.note && warm.on && <span className="text-destructive">{warm.note}</span>}
      {warm.on && (
        <Button size="xs" variant="outline" className="ml-auto" onClick={() => set(s, false)}>
          Arrêter
        </Button>
      )}
    </div>
  )
}

// Statistiques de la session affichée, en haut à droite : cache restant, contexte, tokens, maintien.
export function SessionStat({ s, live }: { s: Session; live: Live }) {
  const a = useActions()
  const warm = useKeepWarm()[s.id]
  const left = a.hotLeft(s)
  return (
    <div className="flex shrink-0 flex-col items-end gap-0.5 text-xs text-muted-foreground tabular-nums" aria-label="Statistiques de la session">
      {a.caps(s).hotCache && (
        <span className={cn("flex items-center gap-1 font-medium", left === null ? "" : left <= 5 ? "text-amber-600 dark:text-amber-400" : "text-orange-500")} title="Le cache des prompts dure une heure après le dernier message">
          {left === null ? <Snowflake className="size-3.5" /> : <Flame className="size-3.5" />}
          {left === null ? "cache froid" : `cache ${left} min`}
          {warm?.on && <Coins className="size-3.5 text-amber-500" aria-label="maintien au chaud actif" />}
        </span>
      )}
      <span title="Tokens traités (hors cache lu)">
        {live.st && live.st.context > 0 && <>ctx {compact(live.st.context)} · </>}
        {compact(tokTotal(s.tok as Tokens))} tok
      </span>
    </div>
  )
}
