import { lazy, Suspense, useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { CircleUserRound, KeyRound, LogOut, ShieldCheck, Users } from "lucide-react"
import { toast } from "sonner"
import { loc } from "@/i18n"
import { boot, logout, signedOut, useAuth, type Reauth } from "@/lib/auth"
import { http, seg } from "@/lib/http"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Enrol, ReauthForm, RecoveryCodes } from "./Mfa"

const AdminDialog = lazy(() => import("./AdminUsers").then((adminUsersModule) => ({ default: adminUsersModule.AdminDialog })))

const MILLISECONDS_PER_SECOND = 1000

export const when = (unixSeconds: number) => new Date(unixSeconds * MILLISECONDS_PER_SECOND).toLocaleString(loc(), { dateStyle: "short", timeStyle: "short" })

// Menu de l'en-tête : rien en mode ouvert.
export function UserMenu() {
  const { t } = useTranslation()
  const auth = useAuth()
  const [open, setOpen] = useState<"" | "profile" | "admin">("")
  if (auth.state !== "ok" || !auth.user) return null
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" aria-label={t("auth.menu.label", { name: auth.user.name })}>
            <CircleUserRound /> <span className="max-w-32 truncate max-md:sr-only">{auth.user.name}</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel className="truncate">{auth.user.name}</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setOpen("profile")}>
            <ShieldCheck /> {t("auth.menu.profile")}
          </DropdownMenuItem>
          {auth.user.admin && (
            <DropdownMenuItem onSelect={() => setOpen("admin")}>
              <Users /> {t("auth.menu.admin")}
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => void logout()}>
            <LogOut /> {t("auth.menu.logout")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ProfileDialog open={open === "profile"} onClose={() => setOpen("")} />
      {open === "admin" && (
        <Suspense fallback={null}>
          <AdminDialog open onClose={() => setOpen("")} />
        </Suspense>
      )}
    </>
  )
}

type Sess = { id: string; created: number; seen: number; ip: string; ua: string; mfa: boolean; current: boolean }
type Pending = { kind: "rmTotp" | "rmKey" | "regen" | "password" | "revoke" | "revokeOthers"; id?: string; key?: number }

export function ProfileDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("auth.profile.title")}</DialogTitle>
          <DialogDescription>{t("auth.profile.sub")}</DialogDescription>
        </DialogHeader>
        {open && <Profile />}
      </DialogContent>
    </Dialog>
  )
}

export function Profile() {
  const { t } = useTranslation()
  const auth = useAuth()
  const factors = auth.factors ?? { totp: false, webauthn: 0, recovery: 0 }
  const [sessions, setSessions] = useState<Sess[] | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)
  const [adding, setAdding] = useState(false)
  const [codes, setCodes] = useState<string[] | null>(null)
  const [pw1, setPw1] = useState("")
  const [pw2, setPw2] = useState("")
  const loadSessions = useCallback(async () => {
    const loadedSessions = await http.get<{ sessions: Sess[] }>("/auth/sessions").then(
      (sessionList) => sessionList.sessions ?? [],
      () => []
    )
    setSessions(loadedSessions)
  }, [])
  useEffect(() => {
    void Promise.resolve().then(loadSessions)
  }, [loadSessions])
  const close = () => {
    setPending(null)
    setPw1("")
    setPw2("")
  }

  const performPendingAction = async (pendingAction: Pending, reauth: Reauth) => {
    if (pendingAction.kind === "rmTotp") {
      await http.post("/auth/factors/totp/removals", reauth)
      return undefined
    }
    if (pendingAction.kind === "rmKey") {
      await http.post(`/auth/factors/webauthn-${pendingAction.key}/removals`, reauth)
      return undefined
    }
    if (pendingAction.kind === "regen") {
      const regenerated = await http.put<{ recovery: string[] }>("/auth/recovery-codes", reauth)
      return regenerated.recovery
    }
    if (pendingAction.kind === "password") {
      await http.put("/auth/password", { ...reauth, new: pw1 })
      return undefined
    }
    if (pendingAction.kind === "revokeOthers") {
      await http.post("/auth/sessions/revocations", reauth)
      return undefined
    }
    await http.post(`/auth/sessions/${seg(pendingAction.id!)}/revocations`, reauth)
    return undefined
  }

  const run = async (reauth: Reauth) => {
    const pendingAction = pending!
    const recoveryCodes = await performPendingAction(pendingAction, reauth)
    close()
    if (recoveryCodes) setCodes(recoveryCodes)
    const revokedCurrentSession =
      pendingAction.kind === "revoke" && sessions?.find((session) => session.id === pendingAction.id)?.current
    if (pendingAction.kind === "password" || revokedCurrentSession) {
      toast.success(t("auth.profile.reconnect"))
      return signedOut()
    }
    toast.success(t("auth.profile.done"))
    await boot()
    const changedSessions = pendingAction.kind === "revoke" || pendingAction.kind === "revokeOthers"
    if (changedSessions) await loadSessions()
  }

  const titles: Record<Pending["kind"], string> = {
    rmTotp: t("auth.profile.removeTotp"),
    rmKey: t("auth.profile.removeKey"),
    regen: t("auth.profile.regenerate"),
    password: t("auth.profile.changePassword"),
    revoke: t("auth.profile.revoke"),
    revokeOthers: t("auth.profile.revokeOthers"),
  }
  const pwMismatch = !!pw2 && pw1 !== pw2

  if (codes) return <RecoveryCodes codes={codes} onDone={() => setCodes(null)} />
  if (adding)
    return (
      <Enrol
        reauth
        hasTotp={factors.totp}
        onCancel={() => setAdding(false)}
        onDone={async () => {
          setAdding(false)
          toast.success(t("auth.profile.done"))
          await boot()
        }}
      />
    )
  return (
    <div className="space-y-5 text-sm">
      {pending && (
        <ReauthForm title={titles[pending.kind]} danger extraOk={pending.kind !== "password" || (pw1.length > 0 && pw1 === pw2)} onSubmit={run} onCancel={close}>
          {pending.kind === "password" && (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="gld-pw-new">{t("auth.profile.newPassword")}</Label>
                <Input id="gld-pw-new" type="password" autoComplete="new-password" value={pw1} onChange={(event) => setPw1(event.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="gld-pw-new2">{t("auth.profile.confirmPassword")}</Label>
                <Input id="gld-pw-new2" type="password" autoComplete="new-password" value={pw2} aria-invalid={pwMismatch} onChange={(event) => setPw2(event.target.value)} />
              </div>
              {pwMismatch && <p className="text-xs text-destructive">{t("auth.profile.mismatch")}</p>}
            </>
          )}
        </ReauthForm>
      )}

      <section className="space-y-2">
        <h3 className="font-medium">{t("auth.profile.factors")}</h3>
        <ul className="divide-y rounded-md border">
          <li className="flex flex-wrap items-center gap-2 p-2">
            <span className="flex-1">{t("auth.profile.totp")}</span>
            <Badge variant={factors.totp ? "default" : "outline"}>{factors.totp ? t("auth.profile.on") : t("auth.profile.off")}</Badge>
            {factors.totp && (
              <Button size="xs" variant="ghost" disabled={!!pending} onClick={() => setPending({ kind: "rmTotp" })}>
                {t("common.delete")}
              </Button>
            )}
          </li>
          <li className="flex flex-wrap items-center gap-2 p-2">
            <span className="flex-1">{t("auth.profile.keys")}</span>
            <Badge variant="outline">{factors.keys?.length ?? factors.webauthn}</Badge>
          </li>
          {factors.keys?.map((securityKey) => (
            <li key={securityKey.id} className="flex flex-wrap items-center gap-2 p-2 pl-6">
              <KeyRound className="size-4 text-muted-foreground" aria-hidden />
              <span className="min-w-0 flex-1 truncate">{securityKey.label || t("auth.profile.keyNoLabel")}</span>
              {securityKey.bad && <Badge variant="destructive">{t("auth.admin.tampered")}</Badge>}
              <span className="text-xs text-muted-foreground">{when(securityKey.created)}</span>
              <Button size="xs" variant="ghost" disabled={!!pending} aria-label={t("auth.profile.removeKeyNamed", { label: securityKey.label })} onClick={() => setPending({ kind: "rmKey", key: securityKey.id })}>
                {t("common.delete")}
              </Button>
            </li>
          ))}
          <li className="flex flex-wrap items-center gap-2 p-2">
            <span className="flex-1">{t("auth.profile.recovery")}</span>
            <Badge variant={factors.recovery ? "outline" : "destructive"}>{factors.recovery}</Badge>
            <Button size="xs" variant="ghost" disabled={!!pending} onClick={() => setPending({ kind: "regen" })}>
              {t("auth.profile.regenerate")}
            </Button>
          </li>
        </ul>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" disabled={!!pending} onClick={() => setAdding(true)}>
            {t("auth.profile.addFactor")}
          </Button>
          <Button size="sm" variant="outline" disabled={!!pending} onClick={() => setPending({ kind: "password" })}>
            {t("auth.profile.changePassword")}
          </Button>
        </div>
      </section>

      <section className="space-y-2">
        <h3 className="font-medium">{t("auth.profile.sessions")}</h3>
        {sessions === null ? (
          <p className="text-muted-foreground">{t("common.loading")}</p>
        ) : (
          <ul className="divide-y rounded-md border">
            {sessions.map((session) => (
              <li key={session.id} className="flex flex-wrap items-center gap-2 p-2">
                <div className="min-w-0 flex-1">
                  <p className="truncate" title={session.ua}>
                    {session.ua || t("auth.profile.unknownUa")}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {session.ip} · {t("auth.profile.seen", { when: when(session.seen) })}
                  </p>
                </div>
                {session.current && <Badge>{t("auth.profile.current")}</Badge>}
                <Button size="xs" variant="ghost" disabled={!!pending} onClick={() => setPending({ kind: "revoke", id: session.id })}>
                  {t("auth.profile.revoke")}
                </Button>
              </li>
            ))}
          </ul>
        )}
        {sessions && sessions.length > 1 && (
          <Button size="sm" variant="outline" disabled={!!pending} onClick={() => setPending({ kind: "revokeOthers" })}>
            {t("auth.profile.revokeOthers")}
          </Button>
        )}
      </section>

      <Button variant="outline" onClick={() => void logout()}>
        <LogOut /> {t("auth.menu.logout")}
      </Button>
    </div>
  )
}
