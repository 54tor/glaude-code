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

const msgOf = (failure: unknown, fallback: string) => {
  if (failure instanceof ApiError && failure.message) return failure.message
  return fallback
}

// Avant une action, la même règle que le serveur : réauthentification pour tout ce qui rend un accès ou écarte un admin.
// Désactiver un compte ordinaire ou couper ses sessions reste immédiat (mesure d'urgence).
function needsReauth(pendingAction: Pending, self: string): boolean {
  switch (pendingAction.kind) {
    case "disable":
      return pendingAction.user.admin || pendingAction.user.tampered
    case "revoke":
      return pendingAction.user.name !== self && (pendingAction.user.admin || pendingAction.user.tampered)
    default:
      return true
  }
}

export function AdminDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
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
    const [usersResult, homesResult] = await Promise.allSettled([http.get<{ users: User[] }>("/admin/users"), http.get<{ homes: Home[] }>("/admin/homes")])
    if (usersResult.status === "fulfilled") setUsers(usersResult.value.users ?? [])
    else toast.error(msgOf(usersResult.reason, t("auth.admin.loadFailed")))
    const loadedHomes = homesResult.status === "fulfilled" ? (homesResult.value.homes ?? []) : []
    const labelMainHome = (home: Home) => {
      if (home.id !== "main") return home
      return { id: home.id, name: t("auth.admin.mainHome") }
    }
    setHomes(loadedHomes.map(labelMainHome))
  }, [t])
  useEffect(() => {
    void Promise.resolve().then(load)
  }, [load])

  const reset = () => {
    setPending(null)
    setConfirmed(false)
    setForm({ name: "", password: "", admin: false })
  }

  const sendPendingAction = async (pendingAction: Pending, reauthPayload: { reauth?: Reauth }) => {
    if (pendingAction.kind === "add") {
      await http.post("/admin/users", { ...reauthPayload, name: form.name, password: form.password, admin: form.admin })
      return
    }
    if (pendingAction.kind === "enable" || pendingAction.kind === "disable") {
      await http.patch(`/admin/users/${pendingAction.user.id}`, { ...reauthPayload, disabled: pendingAction.kind === "disable" })
      return
    }
    if (pendingAction.kind === "resetMfa") {
      await http.post(`/admin/users/${pendingAction.user.id}/mfa-resets`, reauthPayload)
      return
    }
    if (pendingAction.kind === "password") {
      await http.put(`/admin/users/${pendingAction.user.id}/password`, { ...reauthPayload, password: form.password })
      return
    }
    if (pendingAction.kind === "grant") {
      const dangerousConfirmation = DANGEROUS.includes(pendingAction.right) ? { confirm: true } : {}
      await http.put("/admin/acl", { ...reauthPayload, user_id: pendingAction.user.id, home_id: pendingAction.home, grant: [pendingAction.right], ...dangerousConfirmation })
      return
    }
    await http.post(`/admin/sessions/revocations${qs({ user_id: pendingAction.user.id })}`, reauthPayload)
  }

  // Lève ApiError ; sans réauthentification (action immédiate), l'échec s'affiche ici, sinon dans le formulaire.
  const perform = async (pendingAction: Pending, reauth?: Reauth) => {
    const reauthPayload = reauth ? { reauth } : {}
    try {
      await sendPendingAction(pendingAction, reauthPayload)
    } catch (actionError) {
      if (!reauth) toast.error(msgOf(actionError, t("auth.admin.failed")))
      throw actionError
    }
    toast.success(t("auth.admin.done"))
    reset()
    const targetsSelf = "user" in pendingAction && pendingAction.user.name === me
    const closesOwnSessions = pendingAction.kind === "revoke" || pendingAction.kind === "resetMfa" || pendingAction.kind === "password"
    // Le serveur a fermé ses propres sessions (ou effacé son cookie) : l'interface se ferme proprement, comme depuis le profil.
    if (targetsSelf && closesOwnSessions) return signedOut()
    // Ses propres droits changés : useCan suit (boot garde les données, même compte).
    if (targetsSelf && pendingAction.kind === "grant") await boot()
    await load()
  }
  const ask = (pendingAction: Pending) => {
    if (!needsReauth(pendingAction, me)) {
      void perform(pendingAction).catch(() => {})
      return
    }
    setConfirmed(false)
    setPending(pendingAction)
  }

  // Retirer un droit, ou accorder un droit ordinaire à autrui : immédiat. exec, config_write et fs_write : confirmation puis réauthentification ;
  // sessions_read, fs_read et tout droit qu'on s'accorde : réauthentification.
  const toggle = async (targetUser: User, home: string, right: Right, granted: boolean) => {
    const isSensitiveRight = DANGEROUS.includes(right) || right === "sessions_read" || right === "fs_read"
    const grantRequiresReauth = granted && (isSensitiveRight || targetUser.name === me)
    if (grantRequiresReauth) return ask({ kind: "grant", user: targetUser, home, right })
    const aclOperation = granted ? "grant" : "revoke"
    let updatedAcl: { rights: Right[] }
    try {
      updatedAcl = await http.put<{ rights: Right[] }>("/admin/acl", { user_id: targetUser.id, home_id: home, [aclOperation]: [right] })
    } catch (aclError) {
      return void toast.error(msgOf(aclError, t("auth.admin.failed")))
    }
    const withUpdatedHomeRights = (listedUser: User): User => {
      if (listedUser.id !== targetUser.id) return listedUser
      const otherHomesAcl = listedUser.acl.filter((aclRow) => aclRow.home_id !== home)
      const updatedHomeAcl = { home_id: home, home_name: "", rights: updatedAcl.rights ?? [] }
      return { ...listedUser, acl: [...otherHomesAcl, updatedHomeAcl] }
    }
    setUsers((currentUsers) => currentUsers && currentUsers.map(withUpdatedHomeRights))
    if (targetUser.name === me) await boot()
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
  const user = users?.find((listedUser) => listedUser.id === sel)
  const isUnlistedHome = (aclRow: AclRow) => !homes.some((home) => home.id === aclRow.home_id)
  const unlistedHomes = (user?.acl ?? [])
    .filter(isUnlistedHome)
    .map((aclRow) => ({ id: aclRow.home_id, name: aclRow.home_name || aclRow.home_id }))
  const cols = user ? [...homes, ...unlistedHomes] : []
  const extraFieldsValid = (pendingAction: Pending) => {
    if (pendingAction.kind === "grant") return confirmed || !DANGEROUS.includes(pendingAction.right)
    if (pendingAction.kind === "add") return !!form.name && !!form.password
    if (pendingAction.kind === "password") return !!form.password
    return true
  }

  return (
    <div className="space-y-4 text-sm">
      {pending && (
        <ReauthForm
          title={"user" in pending ? t("auth.admin.titleFor", { title: titles[pending.kind], name: pending.user.name }) : titles[pending.kind]}
          danger={pending.kind !== "add" && pending.kind !== "enable"}
          extraOk={extraFieldsValid(pending)}
          focusPassword={pending.kind !== "add"}
          onSubmit={(reauth) => perform(pending, reauth)}
          onCancel={reset}
        >
          {pending.kind === "add" && (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="gld-new-name">{t("auth.login.name")}</Label>
                <Input id="gld-new-name" autoComplete="off" autoFocus value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
              </div>
              <div className="flex items-center gap-2">
                <Checkbox id={adminId} checked={form.admin} onCheckedChange={(checkedState) => setForm({ ...form, admin: checkedState === true })} />
                <Label htmlFor={adminId}>{t("auth.admin.isAdmin")}</Label>
              </div>
            </>
          )}
          {(pending.kind === "add" || pending.kind === "password") && (
            <div className="space-y-1.5">
              <Label htmlFor="gld-new-pw">{t("auth.admin.newPassword")}</Label>
              <Input id="gld-new-pw" type="password" autoComplete="new-password" value={form.password} onChange={(event) => setForm({ ...form, password: event.target.value })} />
            </div>
          )}
          {pending.kind === "grant" && DANGEROUS.includes(pending.right) && (
            <>
              <p className="flex gap-2 font-medium text-destructive">
                <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
                {t("auth.admin.dangerWarning", { right: pending.right, home: cols.find((home) => home.id === pending.home)?.name ?? pending.home })}
              </p>
              <div className="flex items-center gap-2">
                <Checkbox id={confirmId} checked={confirmed} onCheckedChange={(checkedState) => setConfirmed(checkedState === true)} />
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
              {users.map((listedUser) => (
                <tr key={listedUser.id} className="border-t align-middle">
                  <td className="py-1.5">
                    <button type="button" className="font-medium underline-offset-2 hover:underline" onClick={() => setSel(sel === listedUser.id ? null : listedUser.id)} aria-expanded={sel === listedUser.id}>
                      {listedUser.name}
                    </button>{" "}
                    {listedUser.admin && <Badge variant="secondary">{t("auth.admin.admin")}</Badge>} {listedUser.disabled && <Badge variant="outline">{t("auth.admin.disabled")}</Badge>}{" "}
                    {listedUser.tampered && <Badge variant="destructive">{t("auth.admin.tampered")}</Badge>}
                  </td>
                  <td className="text-xs">{t("auth.admin.factors", { totp: listedUser.factors.totp ? t("auth.admin.totp") : t("auth.admin.none"), keys: listedUser.factors.webauthn, codes: listedUser.factors.recovery })}</td>
                  <td className="tabular-nums">{listedUser.sessions}</td>
                  <td className="text-right">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon-sm" aria-label={t("auth.admin.actionsFor", { name: listedUser.name })} disabled={!!pending}>
                          <MoreHorizontal />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onSelect={() => setSel(listedUser.id)}>{t("auth.admin.rights")}</DropdownMenuItem>
                        {listedUser.name !== me && (
                          <DropdownMenuItem onSelect={() => ask({ kind: listedUser.disabled ? "enable" : "disable", user: listedUser })}>{listedUser.disabled ? t("auth.admin.enable") : t("auth.admin.disable")}</DropdownMenuItem>
                        )}
                        <DropdownMenuItem onSelect={() => ask({ kind: "password", user: listedUser })}>{t("auth.admin.setPassword")}</DropdownMenuItem>
                        <DropdownMenuItem onSelect={() => ask({ kind: "revoke", user: listedUser })}>{t("auth.admin.revokeSessions")}</DropdownMenuItem>
                        <DropdownMenuItem variant="destructive" onSelect={() => ask({ kind: "resetMfa", user: listedUser })}>
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
                  {RIGHTS.map((right) => (
                    <th key={right} className={DANGEROUS.includes(right) ? "font-mono font-normal text-destructive" : "font-mono font-normal"}>
                      {right}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {cols.map((home) => {
                  const aclRow = user.acl.find((userAclRow) => userAclRow.home_id === home.id)
                  return (
                    <tr key={home.id} className="border-t">
                      <td className="py-1.5">
                        {home.name} {aclRow?.tampered && <Badge variant="destructive">{t("auth.admin.tampered")}</Badge>}
                      </td>
                      {RIGHTS.map((right) => (
                        <td key={right}>
                          <Checkbox
                            checked={!!aclRow?.rights.includes(right)}
                            disabled={!!pending}
                            aria-label={t("auth.admin.rightFor", { right, home: home.name })}
                            onCheckedChange={(checkedState) => void toggle(user, home.id, right, checkedState === true)}
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
  const fetchPage = useCallback(async (eventKind: string, before?: number) => {
    let pageEvents: Event[] = []
    try {
      const eventPage = await http.get<{ events: Event[] }>(`/admin/events${qs({ limit: PAGE, kind: eventKind, before })}`)
      pageEvents = eventPage.events ?? []
    } catch (loadError) {
      toast.error(msgOf(loadError, t("auth.admin.loadFailed")))
    }
    setMore(pageEvents.length === PAGE)
    return pageEvents
  }, [t])
  useEffect(() => {
    void Promise.resolve().then(() => fetchPage("")).then(setEvents)
  }, [fetchPage])
  const filter = async (event: FormEvent) => {
    event.preventDefault()
    setEvents(await fetchPage(kind.trim()))
  }
  const loadMore = async () => {
    const lastEventId = events?.at(-1)?.id
    const nextEvents = await fetchPage(kind.trim(), lastEventId)
    setEvents((previousEvents) => [...(previousEvents ?? []), ...nextEvents])
  }
  const renderEvents = () => {
    if (events === null) return <p className="text-muted-foreground">{t("common.loading")}</p>
    if (events.length === 0) return <p className="text-muted-foreground">{t("auth.admin.noEvents")}</p>
    return (
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
            {events.map((loginEvent) => (
              <tr key={loginEvent.id} className="border-t">
                <td className="py-1 whitespace-nowrap">{when(loginEvent.at)}</td>
                <td className="max-w-48 truncate" title={loginEvent.name_tried}>
                  {loginEvent.user || loginEvent.name_tried}
                </td>
                <td className="font-mono">{loginEvent.ip}</td>
                <td className="font-mono">{loginEvent.kind}</td>
                <td className="max-w-64 truncate font-mono" title={loginEvent.outcome}>
                  {loginEvent.outcome}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }
  return (
    <div className="space-y-3 text-sm">
      <form onSubmit={filter} className="flex flex-wrap items-end gap-2">
        <div className="space-y-1.5">
          <Label htmlFor="gld-ev-kind">{t("auth.admin.kind")}</Label>
          <Input id="gld-ev-kind" value={kind} onChange={(event) => setKind(event.target.value)} placeholder={t("auth.admin.kindHint")} className="w-48" />
        </div>
        <Button type="submit" size="sm" variant="outline">
          {t("auth.admin.filter")}
        </Button>
      </form>
      {renderEvents()}
      {more && (
        <Button size="sm" variant="outline" onClick={() => void loadMore()}>
          {t("auth.admin.loadMore")}
        </Button>
      )}
    </div>
  )
}
