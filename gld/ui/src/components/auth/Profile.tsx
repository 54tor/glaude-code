import { lazy, Suspense, useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { CircleUserRound, KeyRound, LogOut, ShieldCheck, Users } from "lucide-react"
import { toast } from "sonner"
import { loc } from "@/i18n"
import { authCall, boot, logout, setAuth, useAuth, useWait, waAssert, waAvailable, type AuthRes, type Reauth } from "@/lib/auth"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { CodeInput, Enrol, RecoveryCodes } from "./Mfa"
import { ErrorLine } from "./Login"

const AdminDialog = lazy(() => import("./AdminUsers").then((m) => ({ default: m.AdminDialog })))

export const when = (sec: number) => new Date(sec * 1000).toLocaleString(loc(), { dateStyle: "short", timeStyle: "short" })

// Réponse d'une action protégée : échec de vérification (401, ou 403 sans réauth) en message générique, sinon le message du serveur.
export const outOf = (r: AuthRes, generic: string) => (r.status === 401 || r.status === 403 || !r.body.out ? generic : r.body.out)

// Mot de passe et code (TOTP ou secours), ou mot de passe et clé de sécurité : exigé avant toute action sensible.
export function ReauthForm({
  title,
  children,
  extraOk = true,
  danger,
  onSubmit,
  onCancel,
}: {
  title: string
  children?: ReactNode // champs propres à l'action, au-dessus de la vérification
  extraOk?: boolean
  danger?: boolean
  onSubmit: (r: Reauth) => Promise<AuthRes>
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const { factors } = useAuth()
  const [pw, setPw] = useState("")
  const [code, setCode] = useState("")
  const [err, setErr] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [left, wait] = useWait()
  const send = async (r: Reauth) => {
    setBusy(true)
    setErr(undefined)
    const res = await onSubmit(r)
    setBusy(false)
    if (res.ok) return
    setErr(outOf(res, t("auth.reauth.invalid")))
    wait(res.retry)
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    void send({ password: pw, code: code.trim() })
  }
  // L'assertion validée sur la session vaut code pendant cinq minutes.
  const withKey = async () => {
    setBusy(true)
    const a = await waAssert()
    setBusy(false)
    if (!a.ok) {
      setErr(t("auth.reauth.invalid"))
      return wait(a.retry)
    }
    await send({ password: pw, code: "" })
  }
  const off = busy || left > 0 || !pw || !extraOk
  return (
    <form onSubmit={submit} className="space-y-3 rounded-md border p-3" aria-label={title}>
      <h3 className="text-sm font-medium">{title}</h3>
      {children}
      <p className="text-xs text-muted-foreground">{t("auth.reauth.hint")}</p>
      <div className="space-y-1.5">
        <Label htmlFor="gld-reauth-pw">{t("auth.reauth.password")}</Label>
        <Input id="gld-reauth-pw" type="password" autoComplete="current-password" autoFocus value={pw} onChange={(e) => setPw(e.target.value)} />
      </div>
      <CodeInput id="gld-reauth-code" label={t("auth.reauth.code")} value={code} onChange={setCode} />
      <ErrorLine msg={err} wait={left} />
      <div className="flex flex-wrap gap-2">
        <Button type="submit" size="sm" variant={danger ? "destructive" : "default"} disabled={off || !code.trim()}>
          {t("common.confirm")}
        </Button>
        {!!factors?.webauthn && waAvailable() && (
          <Button type="button" size="sm" variant="outline" disabled={off} onClick={() => void withKey()}>
            <KeyRound /> {t("auth.reauth.key")}
          </Button>
        )}
        <Button type="button" size="sm" variant="ghost" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
      </div>
    </form>
  )
}

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
type Pending = { kind: "add" | "rmTotp" | "regen" | "password" | "revoke"; id?: string }

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
  const [adding, setAdding] = useState<Reauth | null>(null)
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
    if (p.kind === "add") {
      setAdding(r)
      close()
      return { status: 200, ok: true, body: {}, retry: 0 }
    }
    const res =
      p.kind === "rmTotp"
        ? await authCall("/auth/factor/remove", { ...r, kind: "totp", id: 0 })
        : p.kind === "regen"
          ? await authCall<{ recovery: string[] }>("/auth/recovery/regenerate", r)
          : p.kind === "password"
            ? await authCall("/auth/password", { ...r, new: pw1 })
            : await authCall("/auth/sessions/revoke", { ...r, id: p.id })
    if (!res.ok) return res
    close()
    const rec = (res.body as { recovery?: string[] }).recovery
    if (rec) setCodes(rec)
    if (p.kind === "password" || (p.kind === "revoke" && sessions?.find((s) => s.id === p.id)?.current)) {
      toast.success(t("auth.profile.reconnect"))
      setAuth({ state: "anon", csrf: "", user: undefined, expired: false })
      return res
    }
    toast.success(t("auth.profile.done"))
    await boot()
    if (p.kind === "revoke") await loadSessions()
    return res
  }

  const titles: Record<Pending["kind"], string> = {
    add: t("auth.profile.addFactor"),
    rmTotp: t("auth.profile.removeTotp"),
    regen: t("auth.profile.regenerate"),
    password: t("auth.profile.changePassword"),
    revoke: t("auth.profile.revoke"),
  }
  const pwMismatch = !!pw2 && pw1 !== pw2

  if (codes) return <RecoveryCodes codes={codes} onDone={() => setCodes(null)} />
  if (adding)
    return (
      <Enrol
        reauth={adding}
        hasTotp={f.totp}
        onCancel={() => setAdding(null)}
        onDone={async () => {
          setAdding(null)
          toast.success(t("auth.profile.done"))
          await boot()
        }}
      />
    )
  return (
    <div className="space-y-5 text-sm">
      {pending && (
        <ReauthForm title={titles[pending.kind]} danger={pending.kind !== "add"} extraOk={pending.kind !== "password" || (pw1.length > 0 && pw1 === pw2)} onSubmit={run} onCancel={close}>
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
            <Badge variant="outline">{f.webauthn}</Badge>
          </li>
          <li className="flex flex-wrap items-center gap-2 p-2">
            <span className="flex-1">{t("auth.profile.recovery")}</span>
            <Badge variant={f.recovery ? "outline" : "destructive"}>{f.recovery}</Badge>
            <Button size="xs" variant="ghost" disabled={!!pending} onClick={() => setPending({ kind: "regen" })}>
              {t("auth.profile.regenerate")}
            </Button>
          </li>
        </ul>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" disabled={!!pending} onClick={() => setPending({ kind: "add" })}>
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
      </section>

      <Button variant="outline" onClick={() => void logout()}>
        <LogOut /> {t("auth.menu.logout")}
      </Button>
    </div>
  )
}
