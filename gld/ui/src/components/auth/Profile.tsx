import { lazy, Suspense, useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { CircleUserRound, KeyRound, LogOut, ShieldCheck, Users } from "lucide-react"
import { toast } from "sonner"
import { loc } from "@/i18n"
import { authCall, boot, logout, signedOut, useAuth, type AuthRes, type Reauth } from "@/lib/auth"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Enrol, ReauthForm, RecoveryCodes } from "./Mfa"

const AdminDialog = lazy(() => import("./AdminUsers").then((m) => ({ default: m.AdminDialog })))

export const when = (sec: number) => new Date(sec * 1000).toLocaleString(loc(), { dateStyle: "short", timeStyle: "short" })

// Menu de l'en-tête : rien en mode ouvert.
export function UserMenu() {
  const { t } = useTranslation()
  const a = useAuth()
  const [open, setOpen] = useState<"" | "profile" | "admin">("")
  if (a.state !== "ok" || !a.user) return null
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" aria-label={t("auth.menu.label", { name: a.user.name })}>
            <CircleUserRound /> <span className="max-w-32 truncate max-md:sr-only">{a.user.name}</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel className="truncate">{a.user.name}</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setOpen("profile")}>
            <ShieldCheck /> {t("auth.menu.profile")}
          </DropdownMenuItem>
          {a.user.admin && (
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
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
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
  const a = useAuth()
  const f = a.factors ?? { totp: false, webauthn: 0, recovery: 0 }
  const [sessions, setSessions] = useState<Sess[] | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)
  const [adding, setAdding] = useState(false)
  const [codes, setCodes] = useState<string[] | null>(null)
  const [pw1, setPw1] = useState("")
  const [pw2, setPw2] = useState("")
  const loadSessions = useCallback(async () => {
    const r = await authCall<{ sessions: Sess[] }>("/auth/sessions", undefined, "GET")
    setSessions(r.ok ? (r.body.sessions ?? []) : [])
  }, [])
  useEffect(() => {
    void Promise.resolve().then(loadSessions)
  }, [loadSessions])
  const close = () => (setPending(null), setPw1(""), setPw2(""))

  const run = async (r: Reauth): Promise<AuthRes> => {
    const p = pending!
    const res =
      p.kind === "rmTotp"
        ? await authCall("/auth/factor/remove", { ...r, kind: "totp", id: 0 })
        : p.kind === "rmKey"
          ? await authCall("/auth/factor/remove", { ...r, kind: "webauthn", id: p.key })
        : p.kind === "regen"
          ? await authCall<{ recovery: string[] }>("/auth/recovery/regenerate", r)
          : p.kind === "password"
            ? await authCall("/auth/password", { ...r, new: pw1 })
            : p.kind === "revokeOthers"
              ? await authCall("/auth/sessions/revoke-others", r)
              : await authCall("/auth/sessions/revoke", { ...r, id: p.id })
    if (!res.ok) return res
    close()
    const rec = (res.body as { recovery?: string[] }).recovery
    if (rec) setCodes(rec)
    if (p.kind === "password" || (p.kind === "revoke" && sessions?.find((s) => s.id === p.id)?.current)) {
      toast.success(t("auth.profile.reconnect"))
      signedOut()
      return res
    }
    toast.success(t("auth.profile.done"))
    await boot()
    if (p.kind === "revoke" || p.kind === "revokeOthers") await loadSessions()
    return res
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
        hasTotp={f.totp}
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
                <Input id="gld-pw-new" type="password" autoComplete="new-password" value={pw1} onChange={(e) => setPw1(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="gld-pw-new2">{t("auth.profile.confirmPassword")}</Label>
                <Input id="gld-pw-new2" type="password" autoComplete="new-password" value={pw2} aria-invalid={pwMismatch} onChange={(e) => setPw2(e.target.value)} />
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
            <Badge variant={f.totp ? "default" : "outline"}>{f.totp ? t("auth.profile.on") : t("auth.profile.off")}</Badge>
            {f.totp && (
              <Button size="xs" variant="ghost" disabled={!!pending} onClick={() => setPending({ kind: "rmTotp" })}>
                {t("common.delete")}
              </Button>
            )}
          </li>
          <li className="flex flex-wrap items-center gap-2 p-2">
            <span className="flex-1">{t("auth.profile.keys")}</span>
            <Badge variant="outline">{f.keys?.length ?? f.webauthn}</Badge>
          </li>
          {f.keys?.map((k) => (
            <li key={k.id} className="flex flex-wrap items-center gap-2 p-2 pl-6">
              <KeyRound className="size-4 text-muted-foreground" aria-hidden />
              <span className="min-w-0 flex-1 truncate">{k.label || t("auth.profile.keyNoLabel")}</span>
              {k.bad && <Badge variant="destructive">{t("auth.admin.tampered")}</Badge>}
              <span className="text-xs text-muted-foreground">{when(k.created)}</span>
              <Button size="xs" variant="ghost" disabled={!!pending} aria-label={t("auth.profile.removeKeyNamed", { label: k.label })} onClick={() => setPending({ kind: "rmKey", key: k.id })}>
                {t("common.delete")}
              </Button>
            </li>
          ))}
          <li className="flex flex-wrap items-center gap-2 p-2">
            <span className="flex-1">{t("auth.profile.recovery")}</span>
            <Badge variant={f.recovery ? "outline" : "destructive"}>{f.recovery}</Badge>
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
            {sessions.map((s) => (
              <li key={s.id} className="flex flex-wrap items-center gap-2 p-2">
                <div className="min-w-0 flex-1">
                  <p className="truncate" title={s.ua}>
                    {s.ua || t("auth.profile.unknownUa")}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {s.ip} · {t("auth.profile.seen", { when: when(s.seen) })}
                  </p>
                </div>
                {s.current && <Badge>{t("auth.profile.current")}</Badge>}
                <Button size="xs" variant="ghost" disabled={!!pending} onClick={() => setPending({ kind: "revoke", id: s.id })}>
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
