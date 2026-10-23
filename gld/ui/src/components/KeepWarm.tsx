import { Coins, Flame, Snowflake } from "lucide-react"
import { useTranslation } from "react-i18next"
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
  const { t } = useTranslation()
  const a = useActions()
  const warm = useKeepWarm()[s.id]
  if (!a.caps(s).keepWarm) return null // fonction propre à certains fournisseurs
  if (warm?.on)
    return (
      <Button variant="outline" size="sm" className="border-amber-500/60 text-amber-600 dark:text-amber-400" onClick={() => set(s, false)} title={t("conversation.keepWarm.stopTitle")}>
        <Coins /> {t("conversation.keepWarm.active")}
      </Button>
    )
  if (!a.live.has(s.id))
    return (
      <Button variant="ghost" size="sm" disabled title={t("conversation.keepWarm.closedTitle")}>
        <Flame /> {t("conversation.keepWarm.button")}
      </Button>
    )
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" title={t("conversation.keepWarm.buttonTitle")}>
          <Flame /> {t("conversation.keepWarm.button")}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-80">
        <DropdownMenuLabel className="whitespace-normal">{t("conversation.keepWarm.menuLabel")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {[3, 6, 12, 24].map((n) => (
          <DropdownMenuItem key={n} onSelect={() => set(s, true, n)}>
            {t("conversation.keepWarm.atMost", { count: n })}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// Bandeau toujours visible tant que le maintien est actif : c'est de l'argent dépensé sans action de l'utilisateur.
export function KeepWarmBanner({ s }: { s: Session }) {
  const { t } = useTranslation()
  const a = useActions()
  const warm = useKeepWarm()[s.id]
  if (!a.caps(s).keepWarm || (!warm?.on && !warm?.note)) return null
  return (
    <div className={cn("flex flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-1.5 text-xs md:px-6", warm.on ? "border-amber-500/40 bg-amber-500/15 text-amber-700 dark:text-amber-300" : "bg-muted/40 text-muted-foreground")}>
      <Coins className="size-4 shrink-0" />
      {warm.on ? (
        <>
          <span className="font-medium">{t("conversation.keepWarm.bannerActive")}</span>
          <span className="tabular-nums">{t("conversation.keepWarm.pings", { count: warm.pings, pings: warm.pings, max: warm.max })}</span>
          <span>{t("conversation.keepWarm.bannerHint")}</span>
        </>
      ) : (
        <span>{warm.note}</span>
      )}
      {warm.note && warm.on && <span className="text-destructive">{warm.note}</span>}
      {warm.on && (
        <Button size="xs" variant="outline" className="ml-auto" onClick={() => set(s, false)}>
          {t("conversation.keepWarm.stop")}
        </Button>
      )}
    </div>
  )
}

// Statistiques de la session affichée, en haut à droite : cache restant, contexte, tokens, maintien.
export function SessionStat({ s, live }: { s: Session; live: Live }) {
  const { t } = useTranslation()
  const a = useActions()
  const warm = useKeepWarm()[s.id]
  const left = a.hotLeft(s)
  return (
    <div className="flex shrink-0 flex-col items-end gap-0.5 text-xs text-muted-foreground tabular-nums" aria-label={t("conversation.keepWarm.statsAria")}>
      {a.caps(s).hotCache && (
        <span className={cn("flex items-center gap-1 font-medium", left === null ? "" : left <= 5 ? "text-amber-600 dark:text-amber-400" : "text-orange-500")} title={t("conversation.keepWarm.hotTitle")}>
          {left === null ? <Snowflake className="size-3.5" /> : <Flame className="size-3.5" />}
          {left === null ? t("conversation.keepWarm.cold") : t("conversation.keepWarm.left", { min: left })}
          {warm?.on && <Coins className="size-3.5 text-amber-500" aria-label={t("conversation.keepWarm.warmAria")} />}
        </span>
      )}
      <span title={t("conversation.live.tokensTitle")}>
        {live.st && live.st.context > 0 && <>ctx {compact(live.st.context)}&ensp;</>}
        {compact(tokTotal(s.tok as Tokens))} tok
      </span>
    </div>
  )
}
