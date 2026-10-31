import { AlarmClock, Coins, Coffee, Fan, Info, Microwave, Snowflake } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api, type Session } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { cn } from "@/lib/utils"
import { refreshWarm, useKeepWarm } from "@/hooks/useKeepWarm"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useActions } from "./actions"

export const CACHE_EXPIRY_WARNING_MINUTES = 10
const PING_CAP_CHOICES = [3, 6, 12, 24]

const setKeepWarm = async (session: Session, on: boolean, max?: number, reheat?: boolean) => {
  try {
    await api.keepWarmSet({ dir: session.dir, id: session.id, on, max, reheat })
  } catch (error) {
    toast.error(errMsg(error))
  }
  refreshWarm()
}

// L'activation dit d'abord ce que ça coûte, puis propose un plafond de pings.
function Offers({ s: session }: { s: Session }) {
  const { t } = useTranslation()
  const actions = useActions()
  // cache expiré : le premier ping le reconstruit en entier
  const cold = actions.caps(session).hotCache && actions.hotLeft(session) === null
  return (
    <>
      <DropdownMenuLabel className="whitespace-normal">{t(cold ? "conversation.keepWarm.coldLabel" : "conversation.keepWarm.menuLabel")}</DropdownMenuLabel>
      <DropdownMenuSeparator />
      {PING_CAP_CHOICES.map((pingCap) => (
        <DropdownMenuItem key={pingCap} onSelect={() => setKeepWarm(session, true, pingCap, cold)}>
          {t(cold ? "conversation.keepWarm.reheatAtMost" : "conversation.keepWarm.atMost", { count: pingCap })}
        </DropdownMenuItem>
      ))}
    </>
  )
}

// Même commande, en entrée du menu « … » du lecteur sur petit écran.
export function KeepWarmItem({ s: session }: { s: Session }) {
  const { t } = useTranslation()
  const actions = useActions()
  const warm = useKeepWarm()[session.id]
  if (!actions.caps(session).keepWarm) return null
  if (warm?.on)
    return (
      <DropdownMenuItem onSelect={() => setKeepWarm(session, false)}>
        <Microwave className="warm-wave" /> {t("conversation.keepWarm.active")}
      </DropdownMenuItem>
    )
  // un ping part dans un terminal : une tâche de fond n'en a pas
  const cannotPing = !actions.live.has(session.id) || actions.background.has(session.id)
  if (cannotPing)
    return (
      <DropdownMenuItem disabled>
        <Microwave /> {t("conversation.keepWarm.button")}
      </DropdownMenuItem>
    )
  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger>
        <Microwave /> {t("conversation.keepWarm.button")}
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent className="w-72">
        <Offers s={session} />
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  )
}

// Bandeau toujours visible tant que le maintien est actif : c'est de l'argent dépensé sans action de l'utilisateur.
export function KeepWarmBanner({ s: session }: { s: Session }) {
  const { t } = useTranslation()
  const actions = useActions()
  const warm = useKeepWarm()[session.id]
  const hasNothingToShow = !warm?.on && !warm?.note
  if (!actions.caps(session).keepWarm || hasNothingToShow) return null
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
        <Button size="xs" variant="outline" className="ml-auto" onClick={() => setKeepWarm(session, false)}>
          {t("conversation.keepWarm.stop")}
        </Button>
      )}
    </div>
  )
}

// État du cache de la session, en texte qui varie avec l'état. Un clic arrête le maintien s'il est actif,
// sinon ouvre le menu qui dit ce que coûte l'activation.
export function CacheState({ s: session }: { s: Session }) {
  const { t } = useTranslation()
  const actions = useActions()
  const warm = useKeepWarm()[session.id]
  const minutesLeft = actions.hotLeft(session)
  const isCold = minutesLeft === null
  if (!actions.caps(session).hotCache) return null
  const isAsking = actions.asking.has(session.id)
  const isWorking = actions.live.has(session.id) && !actions.waiting.has(session.id)
  const isExpiring = !isCold && minutesLeft <= CACHE_EXPIRY_WARNING_MINUTES
  const isKeptWarm = !!warm?.on
  // Même priorité d'icône que la barre latérale : une seule icône d'état.
  let icon = <Coffee className="size-3.5" />
  let stateLabel = t("conversation.keepWarm.left", { min: minutesLeft })
  if (isAsking) {
    icon = <Info className="size-3.5 animate-pulse" />
    stateLabel = t("shell.sidebar.asking")
  } else if (isWorking) {
    icon = <Fan className="size-3.5 animate-spin" />
    stateLabel = t("shell.sidebar.working")
  } else if (isKeptWarm) {
    icon = <Microwave className="warm-wave size-3.5" />
    stateLabel = t("conversation.keepWarm.stateWarm", { pings: warm.pings, max: warm.max })
  } else if (isExpiring) {
    icon = <AlarmClock className="size-3.5" />
    stateLabel = t("conversation.keepWarm.expiring", { min: minutesLeft })
  } else if (isCold) {
    icon = <Snowflake className="size-3.5" />
    stateLabel = t("conversation.keepWarm.cold")
  }
  const tone = isKeptWarm || (isWorking && !isAsking) ? "text-success" : isCold ? "text-sky-600 dark:text-sky-300" : "text-warning"
  const chip = (
    <>
      {icon}
      <span>{stateLabel}</span>
    </>
  )
  const chipClass = cn("flex items-center gap-1 font-medium", tone)
  const hotTitle = `${t("conversation.keepWarm.hotTitle")} : ${stateLabel}`
  if (!actions.caps(session).keepWarm)
    return (
      <span className={chipClass} title={hotTitle}>
        {chip}
      </span>
    )
  if (isKeptWarm)
    return (
      <Button variant="ghost" size="xs" className={chipClass} onClick={() => setKeepWarm(session, false)} title={t("conversation.keepWarm.stopTitle")}>
        {chip}
      </Button>
    )
  // un ping part dans un terminal : une tâche de fond n'en a pas
  const cannotPing = !actions.live.has(session.id) || actions.background.has(session.id)
  if (cannotPing)
    return (
      <span className={chipClass} title={`${hotTitle}\n${t("conversation.keepWarm.closedTitle")}`}>
        {chip}
      </span>
    )
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="xs" className={chipClass} title={`${hotTitle}\n${t("conversation.keepWarm.buttonTitle")}`}>
          {chip}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-80">
        <Offers s={session} />
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
