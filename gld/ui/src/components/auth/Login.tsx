import { useState, type FormEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { LockKeyhole, ServerCrash } from "lucide-react"
import { authCall, boot, setAuth, useAuth, useWait } from "@/lib/auth"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Enrol, Mfa } from "./Mfa"

// Écran plein, au-dessus de l'application si elle reste montée (session expirée).
export function AuthScreen() {
  const a = useAuth()
  return (
    <div className="fixed inset-0 z-50 overflow-y-auto bg-background">
      <main className="mx-auto flex min-h-full w-full max-w-sm flex-col justify-center px-4 py-10">
        {a.state === "unavailable" ? (
          <Unavailable />
        ) : a.state === "mfa" ? (
          a.enroll ? (
            <Enrol
              onDone={async (csrf) => {
                if (csrf) setAuth({ csrf })
                await boot()
              }}
            />
          ) : (
            <Mfa />
          )
        ) : (
          <Login />
        )}
      </main>
    </div>
  )
}

export function Heading({ children, sub }: { children: ReactNode; sub?: ReactNode }) {
  return (
    <div className="mb-6 space-y-1 text-center">
      <LockKeyhole className="mx-auto size-8 text-muted-foreground" aria-hidden />
      <h1 className="text-xl font-semibold tracking-tight">{children}</h1>
      {sub && <p className="text-sm text-muted-foreground">{sub}</p>}
    </div>
  )
}

// Erreur annoncée par les lecteurs d'écran ; la zone existe toujours pour que l'annonce parte.
export function ErrorLine({ msg, wait }: { msg?: string; wait?: number }) {
  const { t } = useTranslation()
  return (
    <p role="alert" aria-live="assertive" className="min-h-5 text-sm text-destructive">
      {msg}
      {msg && wait ? " " : ""}
      {wait ? t("auth.wait", { n: wait }) : ""}
    </p>
  )
}

export function Login() {
  const { t } = useTranslation()
  const a = useAuth()
  const [name, setName] = useState("")
  const [pw, setPw] = useState("")
  const [err, setErr] = useState(false)
  const [busy, setBusy] = useState(false)
  const [left, wait] = useWait()
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy || left) return
    setBusy(true)
    setErr(false)
    const r = await authCall<{ mfa: string[]; enroll: boolean; csrf: string }>("/auth/login", { name, password: pw })
    setBusy(false)
    if (!r.ok || !r.body.csrf) {
      setErr(true)
      wait(r.retry)
      return
    }
    setPw("")
    setAuth({ state: "mfa", csrf: r.body.csrf, methods: r.body.mfa ?? [], enroll: !!r.body.enroll, user: undefined })
  }
  return (
    <form onSubmit={submit} className="space-y-4" noValidate>
      <Heading sub={a.expired ? t("auth.login.expired") : undefined}>{t("auth.login.title")}</Heading>
      <div className="space-y-1.5">
        <Label htmlFor="gld-login-name">{t("auth.login.name")}</Label>
        <Input id="gld-login-name" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} autoFocus required value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="gld-login-pw">{t("auth.login.password")}</Label>
        <Input id="gld-login-pw" name="password" type="password" autoComplete="current-password" required value={pw} onChange={(e) => setPw(e.target.value)} />
      </div>
      <ErrorLine msg={err ? t("auth.login.invalid") : undefined} wait={left} />
      <Button type="submit" className="w-full" disabled={busy || left > 0 || !name || !pw}>
        {t("auth.login.submit")}
      </Button>
    </form>
  )
}

function Unavailable() {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  return (
    <div className="space-y-4 text-center">
      <ServerCrash className="mx-auto size-8 text-muted-foreground" aria-hidden />
      <h1 className="text-xl font-semibold tracking-tight">{t("auth.unavailable.title")}</h1>
      <p className="text-sm text-muted-foreground">{t("auth.unavailable.text")}</p>
      <Button
        disabled={busy}
        onClick={async () => {
          setBusy(true)
          await boot()
          setBusy(false)
        }}
      >
        {t("common.retry")}
      </Button>
    </div>
  )
}
