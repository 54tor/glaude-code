import { Coins, Coffee, Microwave, Snowflake } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { api, type Session } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { cn } from "@/lib/utils"
import { refreshWarm, useKeepWarm } from "@/hooks/useKeepWarm"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useActions } from "./actions"

const PING_CAP_CHOICES = [3, 6, 12, 24]

const setKeepWarm = async (session: Session, on: boolean, max?: number, reheat?: boolean) => {
  try {
    await api.keepWarmSet({ dir: session.dir, id: session.id, on, max, reheat })
  } catch (error) {
    toast.error(errMsg(error))
  }
  refreshWarm()
}

// Bouton de la barre d'outils : active ou arrête le maintien. L'activation se fait par un menu qui dit ce que ça coûte.
export function KeepWarmButton({ s: session }: { s: Session }) {
  const { t } = useTranslation()
  const actions = useActions()
  const warm = useKeepWarm()[session.id]
  if (!actions.caps(session).keepWarm) return null // fonction propre à certains fournisseurs
  if (warm?.on)
    return (
      <Button variant="outline" size="sm" className="border-warning/60 text-warning" onClick={() => setKeepWarm(session, false)} title={t("conversation.keepWarm.stopTitle")}>
        <Microwave className="warm-wave" /> <span className="max-md:sr-only">{t("conversation.keepWarm.active")}</span>
      </Button>
    )
  // un ping part dans un terminal : une tâche de fond n'en a pas
  const cannotPing = !actions.live.has(session.id) || actions.background.has(session.id)
  if (cannotPing)
    return (
      <Button variant="ghost" size="sm" disabled title={t("conversation.keepWarm.closedTitle")}>
        <Microwave /> <span className="max-md:sr-only">{t("conversation.keepWarm.button")}</span>
      </Button>
    )
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" title={t("conversation.keepWarm.buttonTitle")}>
          <Microwave /> <span className="max-md:sr-only">{t("conversation.keepWarm.button")}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-80">
        <Offers s={session} />
      </DropdownMenuContent>
    </DropdownMenu>
  )
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

// Cache des prompts de la session : minutes restantes ou « froid », et le maintien s'il est actif.
export function CacheState({ s: session }: { s: Session }) {
  const { t } = useTranslation()
  const actions = useActions()
  const warm = useKeepWarm()[session.id]
  const minutesLeft = actions.hotLeft(session)
  const isCold = minutesLeft === null
  if (!actions.caps(session).hotCache) return null
  return (
    <span className={cn("flex items-center gap-1 font-medium", isCold ? "text-sky-600 dark:text-sky-300" : "text-warning")} title={t("conversation.keepWarm.hotTitle")}>
      {isCold ? <Snowflake className="size-3.5" /> : <Coffee className="size-3.5" />}
      {isCold ? t("conversation.keepWarm.cold") : t("conversation.keepWarm.left", { min: minutesLeft })}
      {warm?.on && <Microwave className="warm-wave size-3.5" aria-label={t("conversation.keepWarm.warmAria")} />}
    </span>
  )
}
