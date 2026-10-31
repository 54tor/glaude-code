import { useState } from "react"
import { useTranslation } from "react-i18next"
import { ExternalLink, X } from "lucide-react"
import { toast } from "sonner"
import { api, type InviteInfo, type ShareInfo } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { ago } from "@/lib/format"
import { useShares } from "@/hooks/useShares"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Confirm, type Ask } from "./Confirm"

// Invitations d'une ou de toutes les sessions partagées : état, mode, lien, révocation et messages d'invités en attente.
// only : se limite à cette session (dialogue de partage), qui peut n'avoir encore aucun partage (dir et title servent alors à le créer).
type Props = { only?: { id: string; dir: string; title?: string }; onOpenSession?: (id: string) => void; fast?: boolean }

// Le lien se construit avec le home courant : sans lui il serait faux, et l'action serveur (qui remplace un lien) déjà faite.
// « main » désigne le home du processus principal (share_guest.go).
async function linkBase(t: (key: string) => string): Promise<((token: string) => string) | null> {
  const homesInfo = await api.homes().catch(() => null)
  if (!homesInfo) {
    toast.error(t("shell.share.homesFailed"))
    return null
  }
  return (token) => `${location.origin}/invite/${encodeURIComponent(homesInfo.current || "main")}/${token}`
}

const MAX_INVITES_PER_SESSION = 10

const cleanTitle = (title?: string) => (title && title !== "(sans titre)" ? title.slice(0, 120) : undefined)

export function InvitesPanel({ only, onOpenSession, fast }: Props) {
  const { t } = useTranslation()
  const { shares, error, refresh } = useShares(fast)
  const [busy, setBusy] = useState(false)
  const [ask, setAsk] = useState<Ask | null>(null)

  const guard = (action: () => Promise<void>) => async () => {
    if (busy) return
    setBusy(true)
    try {
      await action()
    } finally {
      setBusy(false)
    }
  }
  const blockContext = { busy, guard, setAsk, refresh, onOpenSession }

  let shownShares = shares
  if (only && shares) {
    const ownShare = shares.find((share) => share.id === only.id)
    shownShares = [ownShare ?? { id: only.id, dir: only.dir, title: only.title ?? "", handle: "", open: true, invites: [] }]
  }

  const renderShares = () => {
    if (!shownShares && error) {
      return (
        <div className="flex items-center gap-2 text-sm">
          <span>{t("shell.share.loadFailed")}</span>
          <Button size="sm" variant="outline" onClick={() => void refresh()}>
            {t("shell.share.retry")}
          </Button>
        </div>
      )
    }
    if (!shownShares) return <p className="text-sm text-muted-foreground">{t("shell.share.loading")}</p>
    if (shownShares.length === 0) return <p className="text-sm text-muted-foreground">{t("shell.invites.none")}</p>
    return (
      <div className="space-y-4">
        {shownShares.map((share) => (
          <SessionBlock key={share.id} s={share} bare={!!only} {...blockContext} />
        ))}
      </div>
    )
  }

  return (
    <>
      {renderShares()}
      <Confirm ask={ask} onClose={() => setAsk(null)} />
    </>
  )
}

type Ctx = { busy: boolean; guard: (action: () => Promise<void>) => () => Promise<void>; setAsk: (ask: Ask) => void; refresh: () => Promise<void>; onOpenSession?: (id: string) => void }

type InviteState = InviteInfo["state"]

function SessionBlock({ s: share, bare, busy, guard, setAsk, refresh, onOpenSession }: Ctx & { s: ShareInfo; bare: boolean }) {
  const { t } = useTranslation()
  const [link, setLink] = useState<{ invite: string; label: string; url: string } | null>(null)
  const [name, setName] = useState("")

  const showLink = async (invite: string, label: string, url: string) => {
    setLink({ invite, label, url })
    try {
      await navigator.clipboard.writeText(url)
      toast.success(t("shell.share.copied"))
    } catch {
      toast.error(t("shell.share.copyFailed"))
    }
  }
  const create = async () => {
    const buildLink = await linkBase(t)
    if (!buildLink) return
    let created: Awaited<ReturnType<typeof api.inviteCreate>>
    try {
      created = await api.inviteCreate(share.id, share.dir, cleanTitle(share.title), name.trim() || undefined)
    } catch (error) {
      return void toast.error(t("shell.share.createFailed", { msg: errMsg(error) }))
    }
    const typedName = name
    setName("")
    await showLink(created.invite?.id ?? "", created.invite?.label ?? typedName.trim(), buildLink(created.token))
    void refresh()
  }
  const regenerate = (invite: InviteInfo) => async () => {
    const buildLink = await linkBase(t)
    if (!buildLink) return
    let regenerated: { token: string }
    try {
      regenerated = await api.inviteRegen(share.id, invite.id)
    } catch (error) {
      return void toast.error(t("shell.invites.regenFailed", { msg: errMsg(error) }))
    }
    await showLink(invite.id, invite.label, buildLink(regenerated.token))
    void refresh()
  }
  const revoke = (invite: InviteInfo) => async () => {
    try {
      await api.inviteRevoke(share.id, invite.id)
    } catch (error) {
      // le serveur révoque en mémoire même quand l'écriture échoue
      toast.error(t("shell.invites.revokeFailed", { msg: errMsg(error) }))
    }
    setLink((currentLink) => (currentLink?.invite === invite.id ? null : currentLink))
    void refresh()
  }
  const toggleMode = (invite: InviteInfo) => async () => {
    try {
      await api.inviteMode(share.id, invite.id, invite.mode === "rw" ? "ro" : "rw")
    } catch (error) {
      return void toast.error(t("shell.share.modeFailed", { msg: errMsg(error) }))
    }
    void refresh()
  }
  const removeQueuedMessage = (item: string) => async () => {
    try {
      await api.queueRm(share.id, item)
    } catch (error) {
      toast.error(t("shell.invites.removeFailed", { msg: errMsg(error) }))
    }
    void refresh()
  }
  const end = async () => {
    try {
      await api.shareEnd(share.id)
    } catch (error) {
      toast.error(t("shell.share.endSaveFailed", { msg: errMsg(error) }))
    }
    setLink(null)
    void refresh()
  }
  const stateDotClass = (state: InviteState) => {
    if (state === "connected") return "bg-green-500"
    if (state === "away") return "bg-amber-500"
    return "bg-muted-foreground/40"
  }
  const stateLabel = (invite: InviteInfo) => {
    if (invite.state === "connected") return t("shell.invites.connected")
    if (invite.state === "away") return t("shell.invites.away", { ago: ago(invite.lastSeen) })
    return t("shell.invites.pending")
  }
  const sessionTitle = share.title || t("shell.invites.sessionUntitled")
  const linkLabel = link?.label ? t("shell.invites.linkOf", { name: link.label }) : t("shell.invites.linkNew")

  return (
    <section className="space-y-2 rounded-md border p-3" aria-label={sessionTitle}>
      {!bare && (
        <div className="flex items-center gap-2">
          <h3 className="min-w-0 flex-1 truncate text-sm font-medium">{sessionTitle}</h3>
          {onOpenSession && (
            <Button size="icon-xs" variant="ghost" aria-label={t("shell.invites.openSession")} onClick={() => onOpenSession(share.id)}>
              <ExternalLink />
            </Button>
          )}
        </div>
      )}
      {!share.open && <p className="text-xs text-warning">{t("shell.invites.closed")}</p>}
      {link && (
        <div className="space-y-1">
          <p className="text-xs text-muted-foreground">{linkLabel}</p>
          <input readOnly value={link.url} aria-label={linkLabel} onFocus={(focusEvent) => focusEvent.target.select()} className="w-full rounded-md border bg-muted px-2 py-1 font-mono text-xs" />
        </div>
      )}
      <ul className="space-y-2">
        {share.invites.map((invite) => (
          <li key={invite.id} className="space-y-1">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="min-w-0 max-w-40 truncate font-medium">{invite.label}</span>
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                <span aria-hidden className={`size-2 rounded-full ${stateDotClass(invite.state)}`} />
                {stateLabel(invite)}
              </span>
              <label className="ml-auto flex items-center gap-1 text-xs">
                <Switch checked={invite.mode === "rw"} disabled={busy} aria-label={t("shell.invites.modeAria", { name: invite.label })} onCheckedChange={() => void guard(toggleMode(invite))()} />
                {invite.mode === "rw" ? t("shell.invites.writeAccess") : t("shell.invites.readOnly")}
              </label>
              <Button
                size="xs"
                variant="outline"
                disabled={busy}
                onClick={() =>
                  setAsk({ title: t("shell.invites.regenTitle", { name: invite.label }), body: t("shell.invites.regenBody"), confirm: t("shell.invites.regenConfirm"), run: guard(regenerate(invite)) })
                }
              >
                {t("shell.invites.regen")}
              </Button>
              <Button
                size="xs"
                variant="destructive"
                disabled={busy}
                onClick={() =>
                  setAsk({ title: t("shell.invites.revokeTitle", { name: invite.label }), body: t("shell.invites.revokeBody"), confirm: t("shell.invites.revokeConfirm"), run: guard(revoke(invite)) })
                }
              >
                {t("shell.invites.revoke")}
              </Button>
            </div>
            {invite.queue.length > 0 && (
              <ul className="space-y-1 text-xs" aria-label={invite.label}>
                {invite.queue.map((queuedMessage) => (
                  <li key={queuedMessage.id} className="flex items-center gap-2 rounded-md border border-dashed px-2 py-1 text-muted-foreground">
                    <span className="min-w-0 flex-1 truncate" title={queuedMessage.text}>
                      {queuedMessage.text}
                    </span>
                    <span className="shrink-0 rounded bg-muted px-1 text-2xs uppercase">{t(`shell.invites.${queuedMessage.state}`)}</span>
                    {queuedMessage.state !== "sending" && (
                      <Button size="icon-xs" variant="ghost" disabled={busy} aria-label={t("shell.invites.removeMsg")} onClick={() => void guard(removeQueuedMessage(queuedMessage.id))()}>
                        <X />
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </li>
        ))}
      </ul>
      {share.invites.length >= MAX_INVITES_PER_SESSION ? (
        <p className="text-xs text-muted-foreground">{t("shell.invites.limit")}</p>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <Input value={name} maxLength={24} aria-label={t("shell.invites.nameAria")} placeholder={t("shell.invites.namePlaceholder")} onChange={(changeEvent) => setName(changeEvent.target.value)} className="h-7 max-w-52" />
          <Button size="sm" disabled={busy} onClick={() => void guard(create)()}>
            {t("shell.invites.invite")}
          </Button>
        </div>
      )}
      {share.invites.length > 0 && bare && (
        <Button
          size="sm"
          variant="destructive"
          disabled={busy}
          onClick={() => setAsk({ title: t("shell.share.endTitle"), body: t("shell.share.endBody"), confirm: t("shell.share.endConfirm"), run: guard(end) })}
        >
          {t("shell.share.end")}
        </Button>
      )}
    </section>
  )
}
