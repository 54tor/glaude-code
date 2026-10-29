import { useCallback, useEffect, useId, useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"
import { MoreHorizontal, TriangleAlert, UserPlus } from "lucide-react"
import { toast } from "sonner"
import { boot, DANGEROUS, RIGHTS, signedOut, useAuth, type Factors, type Reauth, type Right } from "@/lib/auth"
import { ApiError, http, qs } from "@/lib/http"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { when } from "./Profile"
import { ReauthForm } from "./Mfa"

export type AclRow = { home_id: string; home_name: string; rights: Right[]; tampered?: boolean }
export type User = { id: number; name: string; admin: boolean; disabled: boolean; created: number; factors: Factors; sessions: number; acl: AclRow[]; tampered: boolean }
type Event = { id: number; at: number; user_id: number; user: string; name_tried: string; ip: string; kind: string; outcome: string }
type Home = { id: string; name: string }
type Pending =
  | { kind: "add" }
  | { kind: "enable" | "disable" | "resetMfa" | "password" | "revoke"; user: User }
  | { kind: "grant"; user: User; home: string; right: Right }

const PAGE = 100

const msgOf = (e: unknown, fallback: string) => (e instanceof ApiError && e.message ? e.message : fallback)

// Avant une action, la même règle que le serveur : réauthentification pour tout ce qui rend un accès ou écarte un admin.
// Désactiver un compte ordinaire ou couper ses sessions reste immédiat (mesure d'urgence).
function needsReauth(p: Pending, self: string): boolean {
  switch (p.kind) {
    case "disable":
      return p.user.admin || p.user.tampered
    case "revoke":
      return p.user.name !== self && (p.user.admin || p.user.tampered)
    default:
      return true
  }
}

export function AdminDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[92dvh] overflow-y-auto sm:max-w-5xl">
        <DialogHeader>
          <DialogTitle>{t("auth.admin.title")}</DialogTitle>
          <DialogDescription>{t("auth.admin.sub")}</DialogDescription>
        </DialogHeader>
        <Tabs defaultValue="users">
          <TabsList>
            <TabsTrigger value="users">{t("auth.admin.users")}</TabsTrigger>
            <TabsTrigger value="events">{t("auth.admin.events")}</TabsTrigger>
          </TabsList>
          <TabsContent value="users">
            <AdminUsers />
          </TabsContent>
          <TabsContent value="events">
            <AdminEvents />
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}

export function AdminUsers() {
  const { t } = useTranslation()
  const me = useAuth().user?.name ?? ""
  const [users, setUsers] = useState<User[] | null>(null)
  const [homes, setHomes] = useState<Home[]>([])
  const [sel, setSel] = useState<number | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)
  const [form, setForm] = useState({ name: "", password: "", admin: false })
  const [confirmed, setConfirmed] = useState(false)
  const adminId = useId()
  const confirmId = useId()

  const load = useCallback(async () => {
    const [u, h] = await Promise.allSettled([http.get<{ users: User[] }>("/admin/users"), http.get<{ homes: Home[] }>("/admin/homes")])
    if (u.status === "fulfilled") setUsers(u.value.users ?? [])
    else toast.error(msgOf(u.reason, t("auth.admin.loadFailed")))
    setHomes(((h.status === "fulfilled" ? h.value.homes : null) ?? []).map((x) => (x.id === "main" ? { id: x.id, name: t("auth.admin.mainHome") } : x)))
  }, [t])
  useEffect(() => {
    void Promise.resolve().then(load)
  }, [load])

  const reset = () => (setPending(null), setConfirmed(false), setForm({ name: "", password: "", admin: false }))

  // Lève ApiError ; sans réauthentification (action immédiate), l'échec s'affiche ici, sinon dans le formulaire.
  const perform = async (p: Pending, reauth?: Reauth) => {
    const r = reauth ? { reauth } : {}
    try {
      if (p.kind === "add") await http.post("/admin/users", { ...r, name: form.name, password: form.password, admin: form.admin })
      else if (p.kind === "enable" || p.kind === "disable") await http.patch(`/admin/users/${p.user.id}`, { ...r, disabled: p.kind === "disable" })
      else if (p.kind === "resetMfa") await http.post(`/admin/users/${p.user.id}/mfa-resets`, r)
      else if (p.kind === "password") await http.put(`/admin/users/${p.user.id}/password`, { ...r, password: form.password })
      else if (p.kind === "grant") await http.put("/admin/acl", { ...r, user_id: p.user.id, home_id: p.home, grant: [p.right], ...(DANGEROUS.includes(p.right) ? { confirm: true } : {}) })
      else await http.post(`/admin/sessions/revocations${qs({ user_id: p.user.id })}`, r)
    } catch (e) {
      if (!reauth) toast.error(msgOf(e, t("auth.admin.failed")))
      throw e
    }
    toast.success(t("auth.admin.done"))
    reset()
    // Le serveur a fermé ses propres sessions (ou effacé son cookie) : l'interface se ferme proprement, comme depuis le profil.
    if ("user" in p && p.user.name === me && (p.kind === "revoke" || p.kind === "resetMfa" || p.kind === "password")) return signedOut()
    // Ses propres droits changés : useCan suit (boot garde les données, même compte).
    if (p.kind === "grant" && p.user.name === me) await boot()
    await load()
  }
  const ask = (p: Pending) => (needsReauth(p, me) ? (setConfirmed(false), setPending(p)) : void perform(p).catch(() => {}))

  // Retirer un droit, ou accorder un droit ordinaire à autrui : immédiat. exec, config_write et fs_write : confirmation puis réauthentification ;
  // sessions_read, fs_read et tout droit qu'on s'accorde : réauthentification.
  const toggle = async (u: User, home: string, right: Right, on: boolean) => {
    if (on && (DANGEROUS.includes(right) || right === "sessions_read" || right === "fs_read" || u.name === me)) return ask({ kind: "grant", user: u, home, right })
    let res: { rights: Right[] }
    try {
      res = await http.put<{ rights: Right[] }>("/admin/acl", { user_id: u.id, home_id: home, [on ? "grant" : "revoke"]: [right] })
    } catch (e) {
      return void toast.error(msgOf(e, t("auth.admin.failed")))
    }
    setUsers((l) => l && l.map((x) => (x.id !== u.id ? x : { ...x, acl: [...x.acl.filter((a) => a.home_id !== home), { home_id: home, home_name: "", rights: res.rights ?? [] }] })))
    if (u.name === me) await boot()
  }

  const titles: Record<Pending["kind"], string> = {
    add: t("auth.admin.create"),
    enable: t("auth.admin.enable"),
    disable: t("auth.admin.disable"),
    resetMfa: t("auth.admin.resetMfa"),
    password: t("auth.admin.setPassword"),
    revoke: t("auth.admin.revokeSessions"),
    grant: pending?.kind === "grant" && !DANGEROUS.includes(pending.right) ? t("auth.admin.grantRight", { right: pending.right }) : t("auth.admin.grantDangerous"),
  }
  const user = users?.find((u) => u.id === sel)
  const cols = user ? [...homes, ...user.acl.filter((a) => !homes.some((h) => h.id === a.home_id)).map((a) => ({ id: a.home_id, name: a.home_name || a.home_id }))] : []

  return (
    <div className="space-y-4 text-sm">
      {pending && (
        <ReauthForm
          title={"user" in pending ? t("auth.admin.titleFor", { title: titles[pending.kind], name: pending.user.name }) : titles[pending.kind]}
          danger={pending.kind !== "add" && pending.kind !== "enable"}
          extraOk={pending.kind === "grant" ? confirmed || !DANGEROUS.includes(pending.right) : pending.kind === "add" ? !!form.name && !!form.password : pending.kind === "password" ? !!form.password : true}
          focusPassword={pending.kind !== "add"}
          onSubmit={(r) => perform(pending, r)}
          onCancel={reset}
        >
          {pending.kind === "add" && (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="gld-new-name">{t("auth.login.name")}</Label>
                <Input id="gld-new-name" autoComplete="off" autoFocus value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
              </div>
              <div className="flex items-center gap-2">
                <Checkbox id={adminId} checked={form.admin} onCheckedChange={(c) => setForm({ ...form, admin: c === true })} />
                <Label htmlFor={adminId}>{t("auth.admin.isAdmin")}</Label>
              </div>
            </>
          )}
          {(pending.kind === "add" || pending.kind === "password") && (
            <div className="space-y-1.5">
              <Label htmlFor="gld-new-pw">{t("auth.admin.newPassword")}</Label>
              <Input id="gld-new-pw" type="password" autoComplete="new-password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
            </div>
          )}
          {pending.kind === "grant" && DANGEROUS.includes(pending.right) && (
            <>
              <p className="flex gap-2 font-medium text-destructive">
                <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
                {t("auth.admin.dangerWarning", { right: pending.right, home: cols.find((h) => h.id === pending.home)?.name ?? pending.home })}
              </p>
              <div className="flex items-center gap-2">
                <Checkbox id={confirmId} checked={confirmed} onCheckedChange={(c) => setConfirmed(c === true)} />
                <Label htmlFor={confirmId}>{t("auth.admin.dangerConfirm")}</Label>
              </div>
            </>
          )}
        </ReauthForm>
      )}

      <div className="flex justify-end">
        <Button size="sm" disabled={!!pending} onClick={() => ask({ kind: "add" })}>
          <UserPlus /> {t("auth.admin.create")}
        </Button>
      </div>

      {users === null ? (
        <p className="text-muted-foreground">{t("common.loading")}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[36rem]">
            <thead className="text-left text-xs text-muted-foreground">
              <tr>
                <th className="py-1 font-normal">{t("auth.admin.colName")}</th>
                <th className="font-normal">{t("auth.admin.colFactors")}</th>
                <th className="font-normal">{t("auth.admin.colSessions")}</th>
                <th className="font-normal">
                  <span className="sr-only">{t("auth.admin.colActions")}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id} className="border-t align-middle">
                  <td className="py-1.5">
                    <button type="button" className="font-medium underline-offset-2 hover:underline" onClick={() => setSel(sel === u.id ? null : u.id)} aria-expanded={sel === u.id}>
                      {u.name}
                    </button>{" "}
                    {u.admin && <Badge variant="secondary">{t("auth.admin.admin")}</Badge>} {u.disabled && <Badge variant="outline">{t("auth.admin.disabled")}</Badge>}{" "}
                    {u.tampered && <Badge variant="destructive">{t("auth.admin.tampered")}</Badge>}
                  </td>
                  <td className="text-xs">{t("auth.admin.factors", { totp: u.factors.totp ? t("auth.admin.totp") : t("auth.admin.none"), keys: u.factors.webauthn, codes: u.factors.recovery })}</td>
                  <td className="tabular-nums">{u.sessions}</td>
                  <td className="text-right">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon-sm" aria-label={t("auth.admin.actionsFor", { name: u.name })} disabled={!!pending}>
                          <MoreHorizontal />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onSelect={() => setSel(u.id)}>{t("auth.admin.rights")}</DropdownMenuItem>
                        {u.name !== me && (
                          <DropdownMenuItem onSelect={() => ask({ kind: u.disabled ? "enable" : "disable", user: u })}>{u.disabled ? t("auth.admin.enable") : t("auth.admin.disable")}</DropdownMenuItem>
                        )}
                        <DropdownMenuItem onSelect={() => ask({ kind: "password", user: u })}>{t("auth.admin.setPassword")}</DropdownMenuItem>
                        <DropdownMenuItem onSelect={() => ask({ kind: "revoke", user: u })}>{t("auth.admin.revokeSessions")}</DropdownMenuItem>
                        <DropdownMenuItem variant="destructive" onSelect={() => ask({ kind: "resetMfa", user: u })}>
                          {t("auth.admin.resetMfa")}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {user && (
        <section className="space-y-2" aria-label={t("auth.admin.rightsOf", { name: user.name })}>
          <h3 className="font-medium">{t("auth.admin.rightsOf", { name: user.name })}</h3>
          <p className="flex gap-2 text-destructive">
            <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
            {t("auth.admin.aclWarning")}
          </p>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[40rem] text-xs">
              <thead className="text-left text-muted-foreground">
                <tr>
                  <th className="py-1 font-normal">{t("auth.admin.colHome")}</th>
                  {RIGHTS.map((r) => (
                    <th key={r} className={DANGEROUS.includes(r) ? "font-mono font-normal text-destructive" : "font-mono font-normal"}>
                      {r}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {cols.map((h) => {
                  const row = user.acl.find((a) => a.home_id === h.id)
                  return (
                    <tr key={h.id} className="border-t">
                      <td className="py-1.5">
                        {h.name} {row?.tampered && <Badge variant="destructive">{t("auth.admin.tampered")}</Badge>}
                      </td>
                      {RIGHTS.map((r) => (
                        <td key={r}>
                          <Checkbox
                            checked={!!row?.rights.includes(r)}
                            disabled={!!pending}
                            aria-label={t("auth.admin.rightFor", { right: r, home: h.name })}
                            onCheckedChange={(c) => void toggle(user, h.id, r, c === true)}
                          />
                        </td>
                      ))}
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
  )
}

// Journal des connexions : les champs viennent de requêtes anonymes (nom essayé, résultat), toujours rendus en texte.
export function AdminEvents() {
  const { t } = useTranslation()
  const [kind, setKind] = useState("")
  const [events, setEvents] = useState<Event[] | null>(null)
  const [more, setMore] = useState(false)
  const fetchPage = useCallback(async (k: string, before?: number) => {
    let list: Event[] = []
    try {
      list = (await http.get<{ events: Event[] }>(`/admin/events${qs({ limit: PAGE, kind: k, before })}`)).events ?? []
    } catch (e) {
      toast.error(msgOf(e, t("auth.admin.loadFailed")))
    }
    setMore(list.length === PAGE)
    return list
  }, [t])
  useEffect(() => {
    void Promise.resolve().then(() => fetchPage("")).then(setEvents)
  }, [fetchPage])
  const filter = async (e: FormEvent) => {
    e.preventDefault()
    setEvents(await fetchPage(kind.trim()))
  }
  const loadMore = async () => {
    const last = events?.at(-1)?.id
    const next = await fetchPage(kind.trim(), last)
    setEvents((l) => [...(l ?? []), ...next])
  }
  return (
    <div className="space-y-3 text-sm">
      <form onSubmit={filter} className="flex flex-wrap items-end gap-2">
        <div className="space-y-1.5">
          <Label htmlFor="gld-ev-kind">{t("auth.admin.kind")}</Label>
          <Input id="gld-ev-kind" value={kind} onChange={(e) => setKind(e.target.value)} placeholder={t("auth.admin.kindHint")} className="w-48" />
        </div>
        <Button type="submit" size="sm" variant="outline">
          {t("auth.admin.filter")}
        </Button>
      </form>
      {events === null ? (
        <p className="text-muted-foreground">{t("common.loading")}</p>
      ) : events.length === 0 ? (
        <p className="text-muted-foreground">{t("auth.admin.noEvents")}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[40rem] text-xs">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-1 font-normal">{t("auth.admin.colAt")}</th>
                <th className="font-normal">{t("auth.admin.colUser")}</th>
                <th className="font-normal">{t("auth.admin.colIp")}</th>
                <th className="font-normal">{t("auth.admin.kind")}</th>
                <th className="font-normal">{t("auth.admin.colOutcome")}</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e) => (
                <tr key={e.id} className="border-t">
                  <td className="py-1 whitespace-nowrap">{when(e.at)}</td>
                  <td className="max-w-48 truncate" title={e.name_tried}>
                    {e.user || e.name_tried}
                  </td>
                  <td className="font-mono">{e.ip}</td>
                  <td className="font-mono">{e.kind}</td>
                  <td className="max-w-64 truncate font-mono" title={e.outcome}>
                    {e.outcome}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {more && (
        <Button size="sm" variant="outline" onClick={() => void loadMore()}>
          {t("auth.admin.loadMore")}
        </Button>
      )}
    </div>
  )
}
