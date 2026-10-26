import { BarChart3, Coins, Flame, Microwave, Snowflake } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api, type Session, type Tokens } from "@/lib/api"
import { compact, tokTotal } from "@/lib/format"
import { cn } from "@/lib/utils"
import { refreshWarm, useKeepWarm } from "@/hooks/useKeepWarm"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
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
      <Button variant="outline" size="sm" className="border-warning/60 text-warning" onClick={() => set(s, false)} title={t("conversation.keepWarm.stopTitle")}>
        <Microwave className="warm-wave" /> <span className="max-md:sr-only">{t("conversation.keepWarm.active")}</span>
      </Button>
    )
  if (!a.live.has(s.id))
    return (
      <Button variant="ghost" size="sm" disabled title={t("conversation.keepWarm.closedTitle")}>
        <Flame /> <span className="max-md:sr-only">{t("conversation.keepWarm.button")}</span>
      </Button>
    )
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" title={t("conversation.keepWarm.buttonTitle")}>
          <Flame /> <span className="max-md:sr-only">{t("conversation.keepWarm.button")}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-80">
        <Offers s={s} />
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// L'activation dit d'abord ce que ça coûte, puis propose un plafond de pings.
function Offers({ s }: { s: Session }) {
  const { t } = useTranslation()
  return (
    <>
      <DropdownMenuLabel className="whitespace-normal">{t("conversation.keepWarm.menuLabel")}</DropdownMenuLabel>
      <DropdownMenuSeparator />
      {[3, 6, 12, 24].map((n) => (
        <DropdownMenuItem key={n} onSelect={() => set(s, true, n)}>
          {t("conversation.keepWarm.atMost", { count: n })}
        </DropdownMenuItem>
      ))}
    </>
  )
}

// Même commande, en entrée du menu « … » du lecteur sur petit écran.
export function KeepWarmItem({ s }: { s: Session }) {
  const { t } = useTranslation()
  const a = useActions()
  const warm = useKeepWarm()[s.id]
  if (!a.caps(s).keepWarm) return null
  if (warm?.on)
    return (
      <DropdownMenuItem onSelect={() => set(s, false)}>
        <Microwave className="warm-wave" /> {t("conversation.keepWarm.active")}
      </DropdownMenuItem>
    )
  if (!a.live.has(s.id))
    return (
      <DropdownMenuItem disabled>
        <Flame /> {t("conversation.keepWarm.button")}
      </DropdownMenuItem>
    )
  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger>
        <Flame /> {t("conversation.keepWarm.button")}
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="w-72">
        <Offers s={s} />
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  )
}

// Bandeau toujours visible tant que le maintien est actif : c'est de l'argent dépensé sans action de l'utilisateur.
export function KeepWarmBanner({ s }: { s: Session }) {
  const { t } = useTranslation()
  const a = useActions()
  const warm = useKeepWarm()[s.id]
  if (!a.caps(s).keepWarm || (!warm?.on && !warm?.note)) return null
  return (
    <div className={cn("flex flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-1.5 text-xs md:px-6", warm.on ? "border-warning/40 bg-warning/10 text-warning" : "bg-muted/40 text-muted-foreground")}>
      {warm.on ? <Microwave className="warm-wave size-4 shrink-0" /> : <Coins className="size-4 shrink-0" />}
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

// Colonne de droite de l'en-tête : cache restant, état « fermée », bouton « Stats » (contexte, tokens) et maintien au chaud.
export function SessionStat({ s, live, onStats }: { s: Session; live: Live; onStats: () => void }) {
  const { t } = useTranslation()
  const a = useActions()
  const warm = useKeepWarm()[s.id]
  const left = a.hotLeft(s)
  return (
    <div className="flex shrink-0 flex-col items-end gap-0.5 text-xs text-muted-foreground tabular-nums" aria-label={t("conversation.keepWarm.statsAria")}>
      {a.caps(s).hotCache && (
        <span className={cn("flex items-center gap-1 font-medium", left === null ? "" : left <= 5 ? "text-warning" : "text-warning")} title={t("conversation.keepWarm.hotTitle")}>
          {left === null ? <Snowflake className="size-3.5" /> : <Flame className="size-3.5" />}
          {left === null ? t("conversation.keepWarm.cold") : t("conversation.keepWarm.left", { min: left })}
          {warm?.on && <Microwave className="warm-wave size-3.5" aria-label={t("conversation.keepWarm.warmAria")} />}
        </span>
      )}
      <div className="flex items-center gap-1">
        {live.st && !(live.open && live.st.open) && <span className="font-medium">{t("conversation.live.closed")}</span>}
        <Button size="xs" variant="ghost" className="tabular-nums max-md:hidden" onClick={onStats} disabled={!live.st} title={t("conversation.live.statsTitle")}>
          <BarChart3 />
          {t("conversation.live.stats")} :
          {live.st && live.st.context > 0 && <> ctx {compact(live.st.context)}</>} {compact(tokTotal(s.tok as Tokens))} tok
        </Button>
        <span className="max-md:hidden">
          <KeepWarmButton s={s} />
        </span>
      </div>
    </div>
  )
}
