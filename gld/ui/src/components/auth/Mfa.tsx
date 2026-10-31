import { useId, useState, type FormEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import QRCode from "react-qr-code"
import { Copy, Download, KeyRound, Smartphone } from "lucide-react"
import { toast } from "sonner"
import { abandon, boot, setAuth, useAuth, useWait, waAssert, waAvailable, waRegister, type Reauth } from "@/lib/auth"
import { ApiError, http, v1 } from "@/lib/http"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { ErrorLine, Heading } from "./Login"
import { copyText } from "@/lib/utils"

const TOTP_CODE_LENGTH = 6
const FREE_CODE_MAX_LENGTH = 64
const KEY_LABEL_MAX_LENGTH = 64
const QR_CODE_SIZE = 176
const OBJECT_URL_REVOKE_DELAY_MS = 1000
const HTTP_UNAUTHORIZED = 401
const HTTP_FORBIDDEN = 403
const HTTP_CLIENT_ERROR_MIN = 400

const copy = (text: string, successMessage: string) =>
  void copyText(text).then(() => toast.success(successMessage), () => {})

export function CodeInput({ id, label, value, onChange, totp }: { id: string; label: string; value: string; onChange: (localValue: string) => void; totp?: boolean }) {
  const normalizeInput = (rawValue: string) => {
    if (!totp) return rawValue
    return rawValue.replace(/\D/g, "").slice(0, TOTP_CODE_LENGTH)
  }
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={value}
        onChange={(event) => onChange(normalizeInput(event.target.value))}
        autoComplete={totp ? "one-time-code" : "off"}
        inputMode={totp ? "numeric" : "text"}
        pattern={totp ? `[0-9]{${TOTP_CODE_LENGTH}}` : undefined}
        maxLength={totp ? TOTP_CODE_LENGTH : FREE_CODE_MAX_LENGTH}
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
  const fail = async (failure: unknown) => {
    setBusy(false)
    // Trop d'échecs détruisent la session partielle, Retry-After compris : sans elle, inutile d'attendre, retour au mot de passe.
    const isUnauthorized = failure instanceof ApiError && failure.status === HTTP_UNAUTHORIZED
    if (isUnauthorized) {
      const sessionStatus = await fetch(v1("/auth/me")).then((response) => response.status, () => 0)
      if (sessionStatus === HTTP_UNAUTHORIZED) return setAuth({ state: "anon", csrf: "", user: undefined, factors: undefined })
    }
    setErr(true)
    wait(retryOf(failure))
  }
  const run = async (verify: () => Promise<unknown>) => {
    if (busy || left) return
    setBusy(true)
    setErr(false)
    try {
      await verify()
    } catch (verifyError) {
      return fail(verifyError)
    }
    setBusy(false)
    // Le jeton promu arrive par boot(), en même temps que l'état « ok » : avant, l'application encore montée n'envoie rien.
    return boot()
  }
  const submit = (path: string, value: string, clear: () => void) => (event: FormEvent) => {
    event.preventDefault()
    void run(() => http.post(path, { code: value.trim() })).then(clear)
  }
  const off = busy || left > 0
  return (
    <div className="space-y-5">
      <Heading sub={t("auth.mfa.sub")}>{t("auth.mfa.title")}</Heading>
      {methods.includes("totp") && (
        <form onSubmit={submit("/auth/mfa/totp", code, () => setCode(""))} className="space-y-3">
          <CodeInput id="gld-mfa-totp" label={t("auth.mfa.totp")} value={code} onChange={setCode} totp />
          <Button type="submit" className="w-full" disabled={off || code.length !== TOTP_CODE_LENGTH}>
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
      <Button variant="link" className="w-full" onClick={() => void abandon()}>
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
    const downloadLink = document.createElement("a")
    downloadLink.href = URL.createObjectURL(new Blob([text], { type: "text/plain" }))
    downloadLink.download = "gld-recovery-codes.txt"
    document.body.append(downloadLink)
    downloadLink.click()
    downloadLink.remove()
    // Révoquer aussitôt peut annuler le téléchargement dans certains navigateurs.
    setTimeout(() => URL.revokeObjectURL(downloadLink.href), OBJECT_URL_REVOKE_DELAY_MS)
  }
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">{t("auth.codes.title")}</h2>
      <p className="text-sm text-muted-foreground">{t("auth.codes.text")}</p>
      <ul className="grid grid-cols-2 gap-1 rounded-md border bg-muted/40 p-3 font-mono text-sm" aria-label={t("auth.codes.title")}>
        {codes.map((recoveryCode) => (
          <li key={recoveryCode}>{recoveryCode}</li>
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
        <Checkbox id={id} checked={saved} onCheckedChange={(checkedState) => setSaved(checkedState === true)} />
        <Label htmlFor={id}>{t("auth.codes.saved")}</Label>
      </div>
      <Button className="w-full" disabled={!saved} onClick={onDone}>
        {t("auth.codes.continue")}
      </Button>
    </div>
  )
}

// Mot de passe et code (TOTP ou secours), ou mot de passe et clé de sécurité : exigé avant toute action sensible.
export function ReauthForm({
  title,
  children,
  extraOk = true,
  focusPassword = true,
  danger,
  onSubmit,
  onCancel,
}: {
  title: string
  children?: ReactNode // champs propres à l'action, au-dessus de la vérification
  extraOk?: boolean
  focusPassword?: boolean // faux quand un champ propre à l'action vient en premier
  danger?: boolean
  onSubmit: (localReauth: Reauth) => Promise<unknown> // lève ApiError
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const { factors } = useAuth()
  const [pw, setPw] = useState("")
  const [code, setCode] = useState("")
  const [err, setErr] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [left, wait] = useWait()
  const send = async (reauth: Reauth) => {
    setBusy(true)
    setErr(undefined)
    try {
      await onSubmit(reauth)
      setBusy(false)
    } catch (submitError) {
      setBusy(false)
      // Un code accepté ne resert pas : on le redemande à chaque essai.
      setCode("")
      setErr(outOf(submitError, t("auth.reauth.invalid")))
      wait(retryOf(submitError))
    }
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    void send({ password: pw, code: code.trim() })
  }
  // L'assertion validée sur la session vaut code pendant cinq minutes.
  const withKey = async () => {
    setBusy(true)
    try {
      await waAssert()
    } catch (assertionError) {
      setBusy(false)
      setErr(t("auth.reauth.invalid"))
      return wait(retryOf(assertionError))
    }
    setBusy(false)
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
        <Input id="gld-reauth-pw" type="password" autoComplete="current-password" autoFocus={focusPassword} value={pw} onChange={(event) => setPw(event.target.value)} />
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

// Échec d'une action protégée : vérification (401, ou 403 sans réauth) et pannes en message générique, sinon le message du serveur.
export const outOf = (failure: unknown, generic: string) => {
  if (!(failure instanceof ApiError)) return generic
  const isClientOrServerError = failure.status >= HTTP_CLIENT_ERROR_MIN
  const isVerificationFailure = failure.status === HTTP_UNAUTHORIZED || failure.status === HTTP_FORBIDDEN
  if (isClientOrServerError && !isVerificationFailure && failure.message) return failure.message
  return generic
}
export const retryOf = (failure: unknown) => (failure instanceof ApiError ? failure.retry : 0)

// Enrôlement d'un facteur : obligatoire à la première connexion (session partielle), ou depuis le profil (reauth). Depuis le profil,
// chaque début d'enrôlement porte sa propre réauthentification, vérifiée par le serveur avant le QR code ou la clé : un code TOTP ne
// sert qu'une fois, revenir au choix le redemande.
export function Enrol({ reauth, hasTotp, onDone, onCancel }: { reauth?: boolean; hasTotp?: boolean; onDone: () => void; onCancel?: () => void }) {
  const { t } = useTranslation()
  const [step, setStep] = useState<"choose" | "totpAuth" | "totp" | "key">("choose")
  const [totp, setTotp] = useState<{ secret: string; uri: string } | null>(null)
  const [code, setCode] = useState("")
  const [label, setLabel] = useState("")
  const [err, setErr] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [left, wait] = useWait()
  const [codes, setCodes] = useState<string[] | null>(null)
  const first = !reauth
  const fail = (failure: unknown) => {
    setErr(outOf(failure, t("auth.enrol.failed")))
    wait(retryOf(failure))
  }
  const finish = (enrolment: { recovery?: string[] }) => {
    if (enrolment.recovery?.length) setCodes(enrolment.recovery)
    else onDone()
  }
  // Exécute l'appel avec l'état « occupé » ; l'échec s'affiche ici.
  const call = async <Result,>(request: () => Promise<Result>) => {
    setBusy(true)
    setErr(undefined)
    try {
      return await request()
    } catch (requestError) {
      fail(requestError)
    } finally {
      setBusy(false)
    }
  }
  const beginTotp = async (reauth?: Reauth) => {
    const enrolment = await http.post<{ secret: string; uri: string }>("/auth/totp/enrollments", reauth ?? {})
    if (!enrolment.secret || !enrolment.uri) throw new ApiError(0, "bad_response", "")
    setTotp({ secret: enrolment.secret, uri: enrolment.uri })
    setStep("totp")
  }
  const startTotp = () => call(() => beginTotp())
  const chooseTotp = () => {
    if (first) {
      void startTotp()
      return
    }
    setStep("totpAuth")
  }
  const confirm = async (event: FormEvent) => {
    event.preventDefault()
    const enrolment = await call(() => http.put<{ recovery?: string[] }>("/auth/totp/enrollments/current", { code }))
    if (enrolment) finish(enrolment)
    setCode("")
  }
  const register = async (event: FormEvent) => {
    event.preventDefault()
    const registration = await call(() => waRegister(label.trim()))
    if (registration) finish(registration)
  }
  const registerWith = async (reauth: Reauth) => finish(await waRegister(label.trim(), reauth))
  const back = () => {
    setStep("choose")
    setErr(undefined)
  }
  const off = busy || left > 0
  const showsBackButton = step === "totp" || (first && step === "key")
  const showsErrorLine = step !== "totpAuth" && (first || step !== "key")
  const renderFooterButton = () => {
    if (showsBackButton) {
      return (
        <Button variant="ghost" size="sm" onClick={back}>
          {t("common.back")}
        </Button>
      )
    }
    if (step !== "choose" || !onCancel) return null
    return (
      <Button variant="ghost" size="sm" onClick={onCancel}>
        {t("common.cancel")}
      </Button>
    )
  }
  const labelField = (
    <div className="space-y-1.5">
      <Label htmlFor="gld-enrol-label">{t("auth.enrol.label")}</Label>
      <Input id="gld-enrol-label" value={label} maxLength={KEY_LABEL_MAX_LENGTH} autoComplete="off" autoFocus onChange={(event) => setLabel(event.target.value)} placeholder={t("auth.enrol.labelHint")} />
    </div>
  )
  if (codes) return <RecoveryCodes codes={codes} onDone={onDone} />
  return (
    <div className="space-y-4">
      {first ? <Heading sub={t("auth.enrol.sub")}>{t("auth.enrol.title")}</Heading> : <h3 className="text-sm font-medium">{t("auth.enrol.add")}</h3>}
      {step === "choose" && (
        <div className="grid gap-2">
          <Button variant="outline" disabled={off || hasTotp} onClick={chooseTotp}>
            <Smartphone /> {t("auth.enrol.totp")}
          </Button>
          <Button variant="outline" disabled={off || !waAvailable()} onClick={() => setStep("key")}>
            <KeyRound /> {t("auth.enrol.key")}
          </Button>
          {!waAvailable() && <p className="text-sm text-muted-foreground">{t("auth.webauthnUnavailable")}</p>}
        </div>
      )}
      {step === "totpAuth" && <ReauthForm title={t("auth.enrol.totp")} onSubmit={beginTotp} onCancel={back} />}
      {step === "totp" && totp && (
        <form onSubmit={confirm} className="space-y-3">
          <p className="text-sm text-muted-foreground">{t("auth.enrol.scan")}</p>
          <div className="mx-auto w-fit rounded-md bg-white p-3">
            <QRCode value={totp.uri} size={QR_CODE_SIZE} title={t("auth.enrol.qr")} />
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
          <Button type="submit" className="w-full" disabled={off || code.length !== TOTP_CODE_LENGTH}>
            {t("auth.enrol.confirm")}
          </Button>
        </form>
      )}
      {step === "key" &&
        (first ? (
          <form onSubmit={register} className="space-y-3">
            {labelField}
            <Button type="submit" className="w-full" disabled={off || !label.trim()}>
              <KeyRound /> {t("auth.enrol.register")}
            </Button>
          </form>
        ) : (
          <ReauthForm title={t("auth.enrol.key")} extraOk={!!label.trim()} focusPassword={false} onSubmit={registerWith} onCancel={back}>
            {labelField}
          </ReauthForm>
        ))}
      {showsErrorLine && <ErrorLine msg={err} wait={left} />}
      {renderFooterButton()}
      {first && (
        <Button variant="link" className="w-full" onClick={() => void abandon()}>
          {t("auth.mfa.back")}
        </Button>
      )}
    </div>
  )
}
