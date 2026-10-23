import { useId, useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"
import QRCode from "react-qr-code"
import { Copy, Download, KeyRound, Smartphone } from "lucide-react"
import { toast } from "sonner"
import { authCall, boot, logout, setAuth, useAuth, useWait, waAssert, waAvailable, waRegister, type AuthRes, type Reauth } from "@/lib/auth"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { ErrorLine, Heading } from "./Login"

const copy = (text: string, ok: string) => void navigator.clipboard?.writeText(text).then(() => toast.success(ok), () => {})

export function CodeInput({ id, label, value, onChange, totp }: { id: string; label: string; value: string; onChange: (v: string) => void; totp?: boolean }) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={value}
        onChange={(e) => onChange(totp ? e.target.value.replace(/\D/g, "").slice(0, 6) : e.target.value)}
        autoComplete={totp ? "one-time-code" : "off"}
        inputMode={totp ? "numeric" : "text"}
        pattern={totp ? "[0-9]{6}" : undefined}
        maxLength={totp ? 6 : 64}
        autoCapitalize="none"
        spellCheck={false}
        className="font-mono tracking-widest"
      />
    </div>
  )
}

// Second facteur à la connexion : seulement les méthodes enrôlées, et toujours une seule erreur générique.
export function Mfa() {
  const { t } = useTranslation()
  const { methods } = useAuth()
  const [code, setCode] = useState("")
  const [rec, setRec] = useState("")
  const [err, setErr] = useState(false)
  const [busy, setBusy] = useState(false)
  const [left, wait] = useWait()
  const done = async (r: AuthRes<{ csrf: string }>) => {
    setBusy(false)
    if (r.ok) {
      if (r.body.csrf) setAuth({ csrf: r.body.csrf })
      return boot()
    }
    // Sans Retry-After, la session partielle a été détruite (trop d'échecs) : retour au mot de passe.
    if (r.status === 401 && !r.retry) return setAuth({ state: "anon", csrf: "" })
    setErr(true)
    wait(r.retry)
  }
  const run = async (f: () => Promise<AuthRes<{ csrf: string }>>) => {
    if (busy || left) return
    setBusy(true)
    setErr(false)
    await done(await f())
  }
  const submit = (url: string, value: string, clear: () => void) => (e: FormEvent) => {
    e.preventDefault()
    void run(() => authCall(url, { code: value.trim() })).then(clear)
  }
  const off = busy || left > 0
  return (
    <div className="space-y-5">
      <Heading sub={t("auth.mfa.sub")}>{t("auth.mfa.title")}</Heading>
      {methods.includes("totp") && (
        <form onSubmit={submit("/auth/mfa/totp", code, () => setCode(""))} className="space-y-3">
          <CodeInput id="gld-mfa-totp" label={t("auth.mfa.totp")} value={code} onChange={setCode} totp />
          <Button type="submit" className="w-full" disabled={off || code.length !== 6}>
            {t("auth.mfa.verify")}
          </Button>
        </form>
      )}
      {methods.includes("webauthn") &&
        (waAvailable() ? (
          <Button variant="outline" className="w-full" disabled={off} onClick={() => void run(waAssert)}>
            <KeyRound /> {t("auth.mfa.key")}
          </Button>
        ) : (
          <p className="text-sm text-muted-foreground">{t("auth.webauthnUnavailable")}</p>
        ))}
      {methods.includes("recovery") && (
        <>
          <Separator />
          <form onSubmit={submit("/auth/mfa/recovery", rec, () => setRec(""))} className="space-y-3">
            <CodeInput id="gld-mfa-recovery" label={t("auth.mfa.recovery")} value={rec} onChange={setRec} />
            <Button type="submit" variant="outline" className="w-full" disabled={off || !rec.trim()}>
              {t("auth.mfa.useRecovery")}
            </Button>
          </form>
        </>
      )}
      <ErrorLine msg={err ? t("auth.mfa.invalid") : undefined} wait={left} />
      <Button variant="link" className="w-full" onClick={() => void logout()}>
        {t("auth.mfa.back")}
      </Button>
    </div>
  )
}

// Les codes ne sont montrés qu'une fois : il faut cocher « enregistrés » pour continuer.
export function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const { t } = useTranslation()
  const [saved, setSaved] = useState(false)
  const id = useId()
  const text = codes.join("\n") + "\n"
  const download = () => {
    const a = document.createElement("a")
    a.href = URL.createObjectURL(new Blob([text], { type: "text/plain" }))
    a.download = "gld-recovery-codes.txt"
    a.click()
    URL.revokeObjectURL(a.href)
  }
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">{t("auth.codes.title")}</h2>
      <p className="text-sm text-muted-foreground">{t("auth.codes.text")}</p>
      <ul className="grid grid-cols-2 gap-1 rounded-md border bg-muted/40 p-3 font-mono text-sm" aria-label={t("auth.codes.title")}>
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" onClick={() => copy(text, t("common.copied"))}>
          <Copy /> {t("common.copy")}
        </Button>
        <Button variant="outline" size="sm" onClick={download}>
          <Download /> {t("auth.codes.download")}
        </Button>
      </div>
      <div className="flex items-center gap-2">
        <Checkbox id={id} checked={saved} onCheckedChange={(c) => setSaved(c === true)} />
        <Label htmlFor={id}>{t("auth.codes.saved")}</Label>
      </div>
      <Button className="w-full" disabled={!saved} onClick={onDone}>
        {t("auth.codes.continue")}
      </Button>
    </div>
  )
}

// Enrôlement d'un facteur : obligatoire à la première connexion (session partielle), ou depuis le profil avec une réauthentification.
export function Enrol({ reauth, hasTotp, onDone, onCancel }: { reauth?: Reauth; hasTotp?: boolean; onDone: (csrf?: string) => void; onCancel?: () => void }) {
  const { t } = useTranslation()
  const [step, setStep] = useState<"choose" | "totp" | "key">("choose")
  const [totp, setTotp] = useState<{ secret: string; uri: string } | null>(null)
  const [code, setCode] = useState("")
  const [label, setLabel] = useState("")
  const [err, setErr] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [codes, setCodes] = useState<{ list: string[]; csrf?: string } | null>(null)
  const first = !reauth
  const fail = (r: AuthRes) => setErr(r.status === 401 || r.status === 403 || !r.body.out ? t("auth.enrol.failed") : r.body.out)
  const finish = (r: AuthRes<{ csrf: string; recovery: string[] }>) => {
    if (!r.ok) return fail(r)
    if (r.body.recovery?.length) setCodes({ list: r.body.recovery, csrf: r.body.csrf })
    else onDone(r.body.csrf)
  }
  const call = async <T,>(f: () => Promise<AuthRes<T>>) => {
    setBusy(true)
    setErr(undefined)
    const r = await f()
    setBusy(false)
    return r
  }
  const startTotp = async () => {
    const r = await call(() => authCall<{ secret: string; uri: string }>("/auth/totp/begin", reauth ?? {}))
    if (!r.ok || !r.body.secret || !r.body.uri) return fail(r)
    setTotp({ secret: r.body.secret, uri: r.body.uri })
    setStep("totp")
  }
  const confirm = async (e: FormEvent) => {
    e.preventDefault()
    finish(await call(() => authCall<{ csrf: string; recovery: string[] }>("/auth/totp/confirm", { code })))
    setCode("")
  }
  const register = async (e: FormEvent) => {
    e.preventDefault()
    finish(await call(() => waRegister(label.trim(), reauth)))
  }
  if (codes) return <RecoveryCodes codes={codes.list} onDone={() => onDone(codes.csrf)} />
  return (
    <div className="space-y-4">
      {first ? <Heading sub={t("auth.enrol.sub")}>{t("auth.enrol.title")}</Heading> : <h3 className="text-sm font-medium">{t("auth.enrol.add")}</h3>}
      {step === "choose" && (
        <div className="grid gap-2">
          <Button variant="outline" disabled={busy || hasTotp} onClick={() => void startTotp()}>
            <Smartphone /> {t("auth.enrol.totp")}
          </Button>
          <Button variant="outline" disabled={busy || !waAvailable()} onClick={() => setStep("key")}>
            <KeyRound /> {t("auth.enrol.key")}
          </Button>
          {!waAvailable() && <p className="text-sm text-muted-foreground">{t("auth.webauthnUnavailable")}</p>}
        </div>
      )}
      {step === "totp" && totp && (
        <form onSubmit={confirm} className="space-y-3">
          <p className="text-sm text-muted-foreground">{t("auth.enrol.scan")}</p>
          <div className="mx-auto w-fit rounded-md bg-white p-3">
            <QRCode value={totp.uri} size={176} title={t("auth.enrol.qr")} />
          </div>
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">{t("auth.enrol.secret")}</p>
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 rounded bg-muted px-2 py-1 font-mono text-xs break-all">{totp.secret}</code>
              <Button type="button" variant="ghost" size="icon-sm" aria-label={t("common.copy")} onClick={() => copy(totp.secret, t("common.copied"))}>
                <Copy />
              </Button>
            </div>
          </div>
          <CodeInput id="gld-enrol-totp" label={t("auth.enrol.code")} value={code} onChange={setCode} totp />
          <Button type="submit" className="w-full" disabled={busy || code.length !== 6}>
            {t("auth.enrol.confirm")}
          </Button>
        </form>
      )}
      {step === "key" && (
        <form onSubmit={register} className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="gld-enrol-label">{t("auth.enrol.label")}</Label>
            <Input id="gld-enrol-label" value={label} maxLength={64} autoComplete="off" onChange={(e) => setLabel(e.target.value)} placeholder={t("auth.enrol.labelHint")} />
          </div>
          <Button type="submit" className="w-full" disabled={busy || !label.trim()}>
            <KeyRound /> {t("auth.enrol.register")}
          </Button>
        </form>
      )}
      <ErrorLine msg={err} />
      {step !== "choose" ? (
        <Button variant="ghost" size="sm" onClick={() => (setStep("choose"), setErr(undefined))}>
          {t("common.back")}
        </Button>
      ) : (
        onCancel && (
          <Button variant="ghost" size="sm" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
        )
      )}
      {first && (
        <Button variant="link" className="w-full" onClick={() => void logout()}>
          {t("auth.mfa.back")}
        </Button>
      )}
    </div>
  )
}
