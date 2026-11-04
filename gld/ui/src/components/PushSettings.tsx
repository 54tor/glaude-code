import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { useAuth } from "@/lib/auth"
import { errMsg } from "@/lib/apiError"
import { currentPushState, PushPermissionError, pushSupported, subscribePush, unsubscribePush } from "@/lib/push"
import type { PushPreferences, PushState } from "@/lib/api"
import { Switch } from "@/components/ui/switch"

const DEFAULT_PREFERENCES: PushPreferences = { asking: true, waiting: true }

export function PushSection() {
  const { t } = useTranslation()
  const auth = useAuth()
  const [state, setState] = useState<PushState | null>(null)
  const [busy, setBusy] = useState(false)
  const supported = pushSupported()

  useEffect(() => {
    if (!supported) return
    currentPushState().then(setState, () => setState({ subscribed: false, ...DEFAULT_PREFERENCES }))
  }, [supported])

  const apply = async (action: () => Promise<void>, next: PushState) => {
    setBusy(true)
    try {
      await action()
      setState(next)
    } catch (applyError) {
      toast.error(applyError instanceof PushPermissionError ? t("shell.push.denied") : errMsg(applyError) || t("shell.push.failed"))
    } finally {
      setBusy(false)
    }
  }
  const toggleSubscription = (checked: boolean) =>
    checked ? apply(() => subscribePush(DEFAULT_PREFERENCES), { subscribed: true, ...DEFAULT_PREFERENCES }) : apply(unsubscribePush, { subscribed: false, asking: false, waiting: false })
  const togglePreference = (kind: keyof PushPreferences, checked: boolean) => {
    const preferences = { asking: state?.asking ?? true, waiting: state?.waiting ?? true, [kind]: checked }
    return apply(() => subscribePush(preferences), { subscribed: true, ...preferences })
  }

  if (auth.state !== "open" && auth.state !== "ok") return null
  return (
    <section className="space-y-3">
      <h3 className="border-b pb-1 text-md font-semibold">{t("shell.push.title")}</h3>
      <p className="text-sm text-muted-foreground">{t("shell.push.intro")}</p>
      {!supported ? (
        <p className="text-sm text-muted-foreground">{t("shell.push.unsupported")}</p>
      ) : (
        <div className="max-w-sm space-y-3">
          <label className="flex items-center justify-between gap-3 text-sm font-medium">
            {t("shell.push.enable")}
            <Switch checked={state?.subscribed ?? false} disabled={busy || state === null} onCheckedChange={(checked) => void toggleSubscription(checked)} />
          </label>
          <label className="flex items-center justify-between gap-3 text-sm">
            {t("shell.push.asking")}
            <Switch checked={state?.asking ?? false} disabled={busy || !state?.subscribed} onCheckedChange={(checked) => void togglePreference("asking", checked)} />
          </label>
          <label className="flex items-center justify-between gap-3 text-sm">
            {t("shell.push.waiting")}
            <Switch checked={state?.waiting ?? false} disabled={busy || !state?.subscribed} onCheckedChange={(checked) => void togglePreference("waiting", checked)} />
          </label>
        </div>
      )}
    </section>
  )
}
