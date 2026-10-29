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
async function linkBase(t: (k: string) => string): Promise<((token: string) => string) | null> {
  const h = await api.homes().catch(() => null)
  if (!h) {
    toast.error(t("shell.share.homesFailed"))
    return null
  }
  return (token) => `${location.origin}/invite/${encodeURIComponent(h.current || "main")}/${token}`
}

const cleanTitle = (title?: string) => (title && title !== "(sans titre)" ? title.slice(0, 120) : undefined)

export function InvitesPanel({ only, onOpenSession, fast }: Props) {
  const { t } = useTranslation()
  const { shares, error, refresh } = useShares(fast)
  const [busy, setBusy] = useState(false)
  const [ask, setAsk] = useState<Ask | null>(null)

  const guard = (f: () => Promise<void>) => async () => {
    if (busy) return
    setBusy(true)
    try {
      await f()
    } finally {
      setBusy(false)
    }
  }
  const ctx = { busy, guard, setAsk, refresh, onOpenSession }

  let list = shares
  if (only && shares) {
    const own = shares.find((s) => s.id === only.id)
    list = [own ?? { id: only.id, dir: only.dir, title: only.title ?? "", handle: "", open: true, invites: [] }]
  }

  return (
    <>
      {!list ? (
        error ? (
          <div className="flex items-center gap-2 text-sm">
            <span>{t("shell.share.loadFailed")}</span>
            <Button size="sm" variant="outline" onClick={() => void refresh()}>
              {t("shell.share.retry")}
            </Button>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{t("shell.share.loading")}</p>
        )
      ) : list.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("shell.invites.none")}</p>
      ) : (
        <div className="space-y-4">
          {list.map((s) => (
            <SessionBlock key={s.id} s={s} bare={!!only} {...ctx} />
          ))}
        </div>
      )}
      <Confirm ask={ask} onClose={() => setAsk(null)} />
    </>
  )
}

type Ctx = { busy: boolean; guard: (f: () => Promise<void>) => () => Promise<void>; setAsk: (a: Ask) => void; refresh: () => Promise<void>; onOpenSession?: (id: string) => void }

function SessionBlock({ s, bare, busy, guard, setAsk, refresh, onOpenSession }: Ctx & { s: ShareInfo; bare: boolean }) {
  const { t } = useTranslation()
  const [link, setLink] = useState<{ invite: string; label: string; url: string } | null>(null)
  const [name, setName] = useState("")

  const show = async (invite: string, label: string, url: string) => {
    setLink({ invite, label, url })
    try {
      await navigator.clipboard.writeText(url)
      toast.success(t("shell.share.copied"))
    } catch {
      toast.error(t("shell.share.copyFailed"))
    }
  }
  const create = async () => {
    const mk = await linkBase(t)
    if (!mk) return
    const r = await api.inviteCreate(s.id, s.dir, cleanTitle(s.title), name.trim() || undefined)
    if (!r.ok || !r.token) return void toast.error(t("shell.share.createFailed", { msg: r.out }))
    const typed = name
    setName("")
    await show(r.invite?.id ?? "", r.invite?.label ?? typed.trim(), mk(r.token))
    void refresh()
  }
  const regen = (i: InviteInfo) => async () => {
    const mk = await linkBase(t)
    if (!mk) return
    const r = await api.inviteRegen(s.id, i.id)
    if (!r.ok || !r.token) return void toast.error(t("shell.invites.regenFailed", { msg: r.out }))
    await show(i.id, i.label, mk(r.token))
    void refresh()
  }
  const revoke = (i: InviteInfo) => async () => {
    const r = await api.inviteRevoke(s.id, i.id)
    // le serveur révoque en mémoire même quand l'écriture échoue
    if (!r.ok) toast.error(t("shell.invites.revokeFailed", { msg: r.out }))
    setLink((l) => (l?.invite === i.id ? null : l))
    void refresh()
  }
  const mode = (i: InviteInfo) => async () => {
    const r = await api.inviteMode(s.id, i.id, i.mode === "rw" ? "ro" : "rw")
    if (!r.ok) return void toast.error(t("shell.share.modeFailed", { msg: r.out }))
    void refresh()
  }
  const rmMsg = (item: string) => async () => {
    try {
      await api.queueRm(s.id, item)
    } catch (e) {
      toast.error(t("shell.invites.removeFailed", { msg: errMsg(e) }))
    }
    void refresh()
  }
  const end = async () => {
    const r = await api.shareEnd(s.id)
    if (!r.ok) toast.error(t("shell.share.endSaveFailed", { msg: r.out }))
    setLink(null)
    void refresh()
  }

  return (
    <section className="space-y-2 rounded-md border p-3" aria-label={s.title || t("shell.invites.sessionUntitled")}>
      {!bare && (
        <div className="flex items-center gap-2">
          <h3 className="min-w-0 flex-1 truncate text-sm font-medium">{s.title || t("shell.invites.sessionUntitled")}</h3>
          {onOpenSession && (
            <Button size="icon-xs" variant="ghost" aria-label={t("shell.invites.openSession")} onClick={() => onOpenSession(s.id)}>
              <ExternalLink />
            </Button>
          )}
        </div>
      )}
      {!s.open && <p className="text-xs text-warning">{t("shell.invites.closed")}</p>}
      {link && (
        <div className="space-y-1">
          <p className="text-xs text-muted-foreground">{link.label ? t("shell.invites.linkOf", { name: link.label }) : t("shell.invites.linkNew")}</p>
          <input readOnly value={link.url} aria-label={link.label ? t("shell.invites.linkOf", { name: link.label }) : t("shell.invites.linkNew")} onFocus={(e) => e.target.select()} className="w-full rounded-md border bg-muted px-2 py-1 font-mono text-xs" />
        </div>
      )}
      <ul className="space-y-2">
        {s.invites.map((i) => (
          <li key={i.id} className="space-y-1">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <span className="min-w-0 max-w-40 truncate font-medium">{i.label}</span>
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                <span aria-hidden className={`size-2 rounded-full ${i.state === "connected" ? "bg-green-500" : i.state === "away" ? "bg-amber-500" : "bg-muted-foreground/40"}`} />
                {i.state === "connected" ? t("shell.invites.connected") : i.state === "away" ? t("shell.invites.away", { ago: ago(i.lastSeen) }) : t("shell.invites.pending")}
              </span>
              <label className="ml-auto flex items-center gap-1 text-xs">
                <Switch checked={i.mode === "rw"} disabled={busy} aria-label={t("shell.invites.modeAria", { name: i.label })} onCheckedChange={() => void guard(mode(i))()} />
                {i.mode === "rw" ? t("shell.invites.writeAccess") : t("shell.invites.readOnly")}
              </label>
              <Button
                size="xs"
                variant="outline"
                disabled={busy}
                onClick={() =>
                  setAsk({ title: t("shell.invites.regenTitle", { name: i.label }), body: t("shell.invites.regenBody"), confirm: t("shell.invites.regenConfirm"), run: guard(regen(i)) })
                }
              >
                {t("shell.invites.regen")}
              </Button>
              <Button
                size="xs"
                variant="destructive"
                disabled={busy}
                onClick={() =>
                  setAsk({ title: t("shell.invites.revokeTitle", { name: i.label }), body: t("shell.invites.revokeBody"), confirm: t("shell.invites.revokeConfirm"), run: guard(revoke(i)) })
                }
              >
                {t("shell.invites.revoke")}
              </Button>
            </div>
            {i.queue.length > 0 && (
              <ul className="space-y-1 text-xs" aria-label={i.label}>
                {i.queue.map((q) => (
                  <li key={q.id} className="flex items-center gap-2 rounded-md border border-dashed px-2 py-1 text-muted-foreground">
                    <span className="min-w-0 flex-1 truncate" title={q.text}>
                      {q.text}
                    </span>
                    <span className="shrink-0 rounded bg-muted px-1 text-2xs uppercase">{t(`shell.invites.${q.state}`)}</span>
                    {q.state !== "sending" && (
                      <Button size="icon-xs" variant="ghost" disabled={busy} aria-label={t("shell.invites.removeMsg")} onClick={() => void guard(rmMsg(q.id))()}>
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
      {s.invites.length >= 10 ? (
        <p className="text-xs text-muted-foreground">{t("shell.invites.limit")}</p>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <Input value={name} maxLength={24} aria-label={t("shell.invites.nameAria")} placeholder={t("shell.invites.namePlaceholder")} onChange={(e) => setName(e.target.value)} className="h-7 max-w-52" />
          <Button size="sm" disabled={busy} onClick={() => void guard(create)()}>
            {t("shell.invites.invite")}
          </Button>
        </div>
      )}
      {s.invites.length > 0 && bare && (
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
